package servers

import (
	"easyserver/infra/render"
	"easyserver/usecases/routes"
	"fmt"
	"net/http"
)

type  Route struct {
	Path    string   `yaml:"path,omitempty" json:"path,omitempty"`
	Method  string   `yaml:"method,omitempty" json:"method,omitempty"`
	Methods []string `yaml:"methods,omitempty" json:"methods,omitempty"`

	Script      string `yaml:"script,omitempty" json:"script,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`

	ValidateCSRF bool `yaml:"validate_csrf,omitempty" json:"validate_csrf,omitempty"`
	IncludeCSRF  bool `yaml:"include_csrf,omitempty" json:"include_csrf,omitempty"`

	Auth []string `yaml:"auth,omitempty" json:"auth,omitempty"`

	StaticDirConfig     *routes.StaticConfig        `yaml:"static,omitempty" json:"static,omitempty"`
	FileConfig          *routes.FileConfig          `yaml:"file,omitempty" json:"file,omitempty"`
	ForwardConfig       *routes.ForwardConfig       `yaml:"forward,omitempty" json:"forward,omitempty"`
	RedirectConfig      *routes.RedirectConfig      `yaml:"redirect,omitempty" json:"redirect,omitempty"`
	APIConfig           *routes.APIConfig           `yaml:"api,omitempty" json:"api,omitempty"`
	ContentConfig       *routes.ContentConfig       `yaml:"content,omitempty" json:"content,omitempty"`
	AuthLoginConfig     *routes.AuthLoginConfig     `yaml:"auth-login,omitempty" json:"auth_login,omitempty"`
	AuthSignupConfig    *routes.AuthSignupConfig    `yaml:"auth-signup,omitempty" json:"auth_signup,omitempty"`
	AuthLogoutConfig    *routes.AuthLogoutConfig    `yaml:"auth-logout,omitempty" json:"auth_logout,omitempty"`
	StorageAccessConfig *routes.StorageAccessConfig `yaml:"storage-access,omitempty" json:"storage_access,omitempty"`
	DependenciesConfig  *routes.DependenciesConfig  `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	ProcessConfig       *routes.ProcessConfig       `yaml:"process,omitempty" json:"process,omitempty"`
	FileServerConfig    *routes.FileServerConfig    `yaml:"file-server,omitempty" json:"file_server,omitempty"`

	config    RouteConfig // intentionally untagged
	Whitelist []string    `yaml:"ip_whitelist,omitempty" json:"ip_whitelist,omitempty"`
	Blacklist []string    `yaml:"ip_blacklist,omitempty" json:"ip_blacklist,omitempty"`
}

func (r *Route) Validate() error {
	if r.Path == "" {
		return fmt.Errorf("missing path")
	}
	return nil
}
func (r *Route) render(data map[string]string) error {
	var err error
	// args := map[string]any{"args": data}

	// r.FilePath, err = RenderToString(r.FilePath, args)
	// if err != nil {
	// 	return err
	// }

	r.Path, err = render.RenderToString(r.Path, data)
	if err != nil {
		return err
	}

	// r.Dir, err = RenderToString(r.Dir, args)
	// if err != nil {
	// 	return err
	// }

	// r.ForwardURL, err = RenderToString(r.ForwardURL, args)
	// if err != nil {
	// 	return err
	// }

	return nil
}

type RouteConfig interface {
	// Render(data map[string]string) error
	CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error)
	// Validate() error
}

func (route *Route) setRouteConfig() error {
	config, err := route.getRouteConfig()
	if err != nil {
		return err
	}
	if config == nil {
		return fmt.Errorf("missing config fields fpr : '%s'", route.Path)
	}
	route.config = config
	return nil
}

func (route *Route) getRouteConfig() (RouteConfig, error) {
	var routeConfig RouteConfig
	switch route.Type {
	case "static":
		routeConfig = route.StaticDirConfig
		// s.setupStaticRoute(route)
		// StaticDirs = append(StaticDirs, route.Dir)
	case "file":
		routeConfig = route.FileConfig
		// s.setupFileRoute(route)
	case "forward":
		routeConfig = route.ForwardConfig
		// s.setupForwardRoute(route)
	case "api":
		routeConfig = route.APIConfig
		// s.setupAPIRoute(route)
	case "content":
		routeConfig = route.ContentConfig
		// s.setupContentRoute(route)
	case "auth-login":
		routeConfig = route.AuthLoginConfig
		// s.setupAuthLoginRoute(route)
	case "auth-signup":
		routeConfig = route.AuthSignupConfig
		// s.setupAuthSignupRoute(route)
	case "auth-logout":
		routeConfig = route.AuthLogoutConfig
		// s.setupAuthLogoutRoute(route)
	case "storage-access":
		routeConfig = route.StorageAccessConfig
		// s.setupAccessStorageRoute(route)
	case "dependencies":
		routeConfig = route.DependenciesConfig
		// s.setupDependencyRoute(route)
	case "process":
		routeConfig = route.ProcessConfig
		// s.setupProcessRoute(route)
	case "file-server": //, "file_server":
		routeConfig = route.FileServerConfig
		// s.setupFileServerRoute(route)
	default:
		// log.Fatalf("Unknown route type: %s", route.Type)
		return nil, fmt.Errorf("unknown route type: %s", route.Type)
	}

	if routeConfig == nil {
		return nil, fmt.Errorf("missong config field '%s' for path='%s'", route.Type, route.Path)
	}

	return routeConfig, nil
}
