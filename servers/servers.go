package servers

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"easyserver/auth"
	"easyserver/bundler"
	"easyserver/ipfilter"
	"easyserver/net"
	"easyserver/pkg/common"
	"easyserver/routes"
	"easyserver/storage"

	"log"

	"gopkg.in/yaml.v3"
)

type Defaults struct {
	Port *int    `yaml:"port"`
	Host *string `yaml:"host"`
}

type HTTPSConfig struct {
	SSLKeyfile   string   `json:"ssl_keyfile" yaml:"ssl_keyfile"`
	SSLCertfile  string   `json:"ssl_certfile" yaml:"ssl_certfile"`
	Generate     bool     `json:"generate" yaml:"generate"`
	Organization []string `json:"organization" yaml:"organization"`
	DNSNames     []string `json:"dns_names" yaml:"dns_names"`

	CommonName string `json:"common_name" yaml:"common_name"`
}

type Arg struct {
	Default     *string `yaml:"default" json:"default"`
	Description string  `yaml:"description" json:"description"`
}

type Config struct {
	JSONDiscoveryRoutePath string `yaml:"json_discovery_route_path" json:"json_discovery_route_path"`
	HTMLDiscoveryRoutePath string `yaml:"html_discovery_route_path" json:"html_discovery_route_path"`

	Defaults *Defaults `yaml:"default" json:"default"`

	Storage     map[string]*storage.StorageConfig `json:"storage,omitempty"`
	HTTPSConfig *HTTPSConfig                      `yaml:"https_config,omitempty" json:"https_config,omitempty"`
	Args        map[string]*Arg                   `yaml:"args,omitempty" json:"args,omitempty"`
	Env         map[string]*Arg                   `yaml:"env,omitempty" json:"env,omitempty"`

	Auth      map[string]*auth.AuthConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
	Build     *bundler.Config             `yaml:"build,omitempty" json:"build,omitempty"`
	RawRoutes RawYAML                     `yaml:"routes,omitempty" json:"-,omitempty"`
	Routes    []*Route                    `yaml:"-" json:"Routes"`

	IpFilter *struct {
		Whitelist []string `yaml:"ip_whitelist,omitempty" json:"ip_whitelist,omitempty"`
		Blacklist []string `yaml:"ip_blacklist,omitempty" json:"ip_blacklist,omitempty"`
	} `yaml:"ip_filter,omitempty" json:"ip_filter,omitempty"`
}

// Server struct
type Server struct {
	Config  *Config
	mux     *http.ServeMux
	Address string
	Args    map[string]string

	// storageRefs map[string]storage.StorageRef
	Debug bool
}

func (s *Server) HandleFunc(route *Route) error {
	pattern := strings.TrimSpace(fmt.Sprintf("%s %s", route.Method, route.Path))
	log.Printf("registering route: pattern=%q auth=%v csrfValidate=%v csrfInclude=%v",
		pattern, route.Auth, route.ValidateCSRF, route.IncludeCSRF)

	handler, err := route.config.CreateRoute(route.Method, route.Path, s.Args)
	if err != nil {
		log.Printf("route creation failed: pattern=%q err=%v", pattern, err)
		return err
	}

	allowedMethods := []string{}
	route.Methods = append(route.Methods, route.Method)
	for _, method := range route.Methods {
		m := strings.ToUpper(strings.TrimSpace(method))
		if m == "" {
			continue
		}
		allowedMethods = append(allowedMethods, m)
	}

	var wrappedHandler http.HandlerFunc

	if len(allowedMethods) > 0 {
		log.Printf("route=%q allowedMethods=%v", pattern, allowedMethods)
		wrappedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("incoming request: %s %s", r.Method, r.URL.Path)
			if !slices.Contains(allowedMethods, r.Method) {
				log.Printf("method not allowed: route=%q method=%s allowed=%v", pattern, r.Method, allowedMethods)
				http.Error(w, fmt.Sprintf("method not allowed: %s", r.Method), http.StatusMethodNotAllowed)
				return
			}
			handler(w, r)
		})
	} else {
		log.Printf("route=%q has no allowedMethods; using raw handler", pattern)
		wrappedHandler = http.HandlerFunc(handler)
	}

	if route.ValidateCSRF {
		log.Printf("route=%q applying CSRF validation middleware", pattern)
		wrappedHandler = s.csrfMiddleware(wrappedHandler)
	} else if route.IncludeCSRF {
		log.Printf("route=%q applying CSRF inclusion middleware", pattern)
		prev := wrappedHandler
		wrappedHandler = func(w http.ResponseWriter, r *http.Request) {
			s.IncludeCSRFToken(w, r)
			prev(w, r)
		}
	}

	if len(route.Auth) > 0 {
		log.Printf("route=%q applying auth middleware roles=%v", pattern, route.Auth)
		wrappedHandler = auth.RequireAuth(wrappedHandler, route.Auth...).ServeHTTP
	}

	if len(route.Whitelist) > 0 || len(route.Blacklist) > 0 {

		log.Printf("route=%q applying IP filters: whitelist=%v blacklist=%v",
			pattern, route.Whitelist, route.Blacklist)

		filter := ipfilter.NewIPFilterCombined(route.Whitelist, route.Blacklist)

		wrappedHandler = filter.MiddlewareFunc(wrappedHandler)
	}

	log.Printf("finalizing route registration: %q", pattern)
	s.mux.HandleFunc(pattern, wrappedHandler)

	return nil
}

func NewStaticServer(path string, args []string) (*Server, error) {
	var server Server
	argCmd := flag.NewFlagSet("args", flag.ExitOnError)
	host := argCmd.String("host", "localhost", "Host to listen on")
	port := argCmd.Int("port", 12344, "Port to listen on")
	relPath := argCmd.String("path", "/", "url path. defaults to '/'")

	fmt.Println(path)
	fmt.Println(args)

	if err := argCmd.Parse(args[1:]); err != nil {
		return nil, fmt.Errorf("failed to parse args: %v", err.Error())
	}

	server.Config = &Config{
		Routes: []*Route{
			{
				Type: "static",
				Path: *relPath,
				StaticDirConfig: &routes.StaticConfig{
					Dir: path,
				},
			},
		},
	}

	fmt.Println("len(config): ", len(server.Config.Routes))

	hostIp := net.ProcessHost(*host)

	address := fmt.Sprintf("%s:%v", hostIp, *port)

	fmt.Println("ADDRESS: ", address)

	server.Address = address
	server.Args = map[string]string{}
	mux := http.NewServeMux()
	server.mux = mux

	return &server, nil
}

func NewHTMLServer(path string, args []string) (*Server, error) {
	var server Server
	argCmd := flag.NewFlagSet("args", flag.ExitOnError)
	static := argCmd.String("static", "", "Static [dir]:[path]")
	host := argCmd.String("host", "localhost", "Host to listen on")
	port := argCmd.Int("port", 2344, "Port to listen on")

	// err := os.Chdir(filepath.Dir(path))
	// if err != nil {
	// 	return nil, err
	// }

	fmt.Println(path)
	fmt.Println(args)

	if err := argCmd.Parse(args[1:]); err != nil {
		return nil, fmt.Errorf("failed to parse args: %v", err.Error())
	}

	config := Config{
		Routes: []*Route{},
	}

	config.Routes = append(config.Routes, &Route{
		Type: "file",
		Path: "/",
		FileConfig: &routes.FileConfig{
			FilePath: path,
		},
	})
	parts := strings.SplitN(*static, ":", 2)
	switch len(parts) {
	case 1:
		config.Routes = append(config.Routes, &Route{
			Type: "static",
			Path: "/static",
			StaticDirConfig: &routes.StaticConfig{
				Dir: strings.TrimSpace(parts[0]),
			},
		})
	case 2:
		config.Routes = append(config.Routes, &Route{
			Type: "static",
			Path: strings.TrimSpace(parts[1]),
			StaticDirConfig: &routes.StaticConfig{
				Dir: strings.TrimSpace(parts[0]),
			},
		})
	}

	server.Config = &config

	hostIp := net.ProcessHost(*host)

	address := fmt.Sprintf("%s:%v", hostIp, *port)

	fmt.Println("ADDRESS: ", address)

	server.Address = address
	server.Args = map[string]string{}
	mux := http.NewServeMux()
	server.mux = mux

	return &server, nil
}

type HandleFuncWrapper struct {
	HandlerFunc func(http.ResponseWriter, *http.Request)
}

func NewHandleFuncWrapper(handlerFunc func(http.ResponseWriter, *http.Request)) *HandleFuncWrapper {
	return &HandleFuncWrapper{
		HandlerFunc: handlerFunc,
	}
}

func (h *HandleFuncWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.HandlerFunc(w, r)
}

func NewServer(configPath string) (*Server, error) {

	os.Chdir(filepath.Dir(configPath))
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	mux := http.NewServeMux()

	return &Server{Config: config, mux: mux}, nil
}

func (s *Server) InitDependencies() error {
	config := s.Config

	if config.Storage != nil {
		err := storage.InitStorage(config.Storage)
		if err != nil {
			fmt.Println("InitStorage err: ", err.Error())
			return err
		}
	}

	if config.Auth != nil {
		err := auth.InitAuthManager(config.Auth)
		if err != nil {
			return err
		}
	}

	return nil
}

func loadConfig(configPath string) (*Config, error) {

	bytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	configYAML := string(bytes)

	var config Config
	err = yaml.Unmarshal([]byte(configYAML), &config)
	if err != nil {
		fmt.Println(configYAML)
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

type RouteSummary struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Method      string `json:"method"`
}

func (s *Server) renderVars() error {

	var routes = []*Route{}

	routeBytes, err := s.Config.RawRoutes.Bytes()
	if err != nil {
		return err
	}
	defer func() {
		s.Config.RawRoutes = RawYAML{}
	}()

	routeString := string(routeBytes)
	for key, value := range s.Args {
		routeString = strings.ReplaceAll(routeString, fmt.Sprintf("$%s", key), value)
		fmt.Println("ADDED ARG VALUE FOR: ", key, " -> ", value)
	}

	for key, valueConfig := range s.Config.Env {
		value := os.Getenv(key)
		if value == "" && valueConfig.Default != nil && *valueConfig.Default != "" {
			value = *valueConfig.Default
			os.Setenv(key, value)
			fmt.Printf("USING DEFAULT VALUE FOR ENV VAR: %s\n", key)
		}
		if value == "" {
			return fmt.Errorf("missing env value for: %s", key)
		}
		routeString = strings.ReplaceAll(routeString, fmt.Sprintf("$%s", key), value)
		fmt.Println("ADDED ENV VALUE FOR: ", key)
	}

	err = yaml.Unmarshal([]byte(routeString), &routes)
	if err != nil {
		return err
	}

	s.Config.Routes = append(s.Config.Routes, routes...)
	return nil

}

func (s *Server) Start(ctx context.Context) error {
	if s.Config.Build != nil {
		if err := bundler.Run(*s.Config.Build); err != nil {
			return fmt.Errorf("frontend build failed: %w", err)
		}
		if s.Config.Build.Watch {
			bundler.StartWatcher(*s.Config.Build, ctx)
		}
		s.Config.Routes = append(s.Config.Routes, &Route{
			Type: "static",
			Path: "/",
			StaticDirConfig: &routes.StaticConfig{Dir: s.Config.Build.DistDir},
		})
	}

	err := s.renderVars()
	if err != nil {
		return err
	}

	err = s.InitDependencies()
	if err != nil {
		return err
	}

	address := s.Address
	args := s.Args

	// common.PrintJSON(common.Object{"INNER": s.Config})

	fmt.Println("ROUTES count: ", len(s.Config.Routes))

	routes := []RouteSummary{}

	for _, route := range s.Config.Routes {

		err := route.render(args)
		if err != nil {
			return err
		}

		method := strings.ToUpper(strings.TrimSpace(route.Method))
		if method == "" {
			method = "GET"
		}

		routes = append(routes, RouteSummary{
			Path:        route.Path,
			Type:        route.Type,
			Description: route.Description,
			Method:      method,
		})

		fmt.Printf("Setting up path: '%s' for '%s'\n", route.Path, route.Type)

		err = route.setRouteConfig()
		if err != nil {
			return err
		}

		err = route.Validate()
		if err != nil {
			return err
		}

		err = s.HandleFunc(route)
		if err != nil {
			return err
		}
	}

	if s.Config.JSONDiscoveryRoutePath != "" {
		w := strings.Builder{}
		encoder := json.NewEncoder(&w)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "    ")
		data, _ := json.Marshal(routes)
		s.mux.HandleFunc(s.Config.JSONDiscoveryRoutePath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "application/json")
			w.Write(data)
		})
	}
	
	if s.Config.HTMLDiscoveryRoutePath != "" {

		s.mux.HandleFunc(s.Config.HTMLDiscoveryRoutePath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "text/html")
			renderDiscovery(w, routes)
		})
	}

	// if s.debug {
	// 	s.initDebug()
	// }

	var handler http.Handler

	if s.Config.IpFilter != nil {
		filter := ipfilter.NewIPFilterCombined(s.Config.IpFilter.Whitelist, s.Config.IpFilter.Blacklist)

		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, clientIP := filter.CheckRequest(r)
			if !allowed {
				log.Printf("Blocked request from IP: %s", clientIP)
				http.Error(w, "Access denied", http.StatusForbidden)
				return
			}
			s.mux.ServeHTTP(w, r)
		})

		handler = loggingMiddleware(handler)

	} else {
		handler = loggingMiddleware(s.mux)
	}

	srv := &http.Server{
		Addr:    address,
		Handler: handler,
	}

	if ctx != nil {
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			srv.Shutdown(shutdownCtx)
		}()
	}
	if s.Config.HTTPSConfig != nil {
		if s.Config.HTTPSConfig.Generate && !common.PathExists(s.Config.HTTPSConfig.SSLCertfile) {
			err = generateHTTPS(s.Config.HTTPSConfig)
			if err != nil {
				return err
			}
		}
		log.Printf("Server starting https://%s", address)
		err = srv.ListenAndServeTLS(
			s.Config.HTTPSConfig.SSLCertfile,
			s.Config.HTTPSConfig.SSLKeyfile,
		)
	} else {
		log.Printf("Server starting http://%s", address)
		err = srv.ListenAndServe()
	}

	// Ignore "server closed" errors
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// var SSR_SCRIPT_PATH string

// func (s *Server) initDebug() {
// 	SSR_SCRIPT_PATH = InitSSrFileChange(s)
// }
