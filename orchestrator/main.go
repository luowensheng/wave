package main

import (
	"context"
	"easyserver/net"
	"easyserver/servers"

	log "easyserver/infra/logger"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {

	// Ensure the command is "serve" and path is provided
	if len(os.Args) < 2 {
		log.Fatal("Usage: autoserver serve [path] --host [host] --port [port] [--key value]...")
	}
	switch os.Args[1] {
	case "serve", "run":
		serve()
	case "serve-live":
		servelive()
	case "version":
		fmt.Println("1.0.1")
	// case "help-routes":
	// 	helpRoutes()
	default:
		log.Fatalf("Invalid command: '%s'", os.Args[1])
	}
}

func servelive() {
	var APPCONTEXT context.Context
	var APPCANCEL context.CancelFunc
	path := os.Args[2]
	lastModTime := time.Time{}
	for {
		info, err := os.Stat(path)
		if err != nil {
			log.Fatal(err)
		}

		modtime := info.ModTime()
		if modtime.After(lastModTime) {
			server, err := loadServer()
			if err == nil {
				if APPCANCEL != nil {
					APPCANCEL() // signal shutdown
				}
				APPCONTEXT, APPCANCEL = context.WithCancel(context.Background())
				fmt.Println("Restarting Server...")
				go func() {
					defer func() {
						if err := recover(); err != nil {
							return
						}
					}()
					if err := server.Start(APPCONTEXT); err != nil {
						log.Fatalf("Server failed: %v", err.Error())
					}
				}()
				time.Sleep(1 * time.Second)
			} else {
				fmt.Println("SERVER ERR: ", err.Error())
			}

		}
		lastModTime = modtime
	}
}

func serve() {
	fmt.Println("--------------------------------------")

	server, err := loadServer()
	if err != nil {
		log.Fatal(err.Error())
	}
	// common.PrintJSON(server.config)
	if err := server.Start(context.Background()); err != nil {
		log.Fatalf("Server failed: %v", err.Error())
	}
}

func loadServer() (*servers.Server, error) {
	path, err := filepath.Abs(os.Args[2])
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".html") {
		fmt.Println("HTML APP SERVER")

		return servers.NewHTMLServer(path, os.Args[2:])
	}

	if info, err := os.Stat(path); err == nil && info.IsDir() {
		fmt.Println("STATIC SERVER")
		return servers.NewStaticServer(path, os.Args[2:])
	}
	fmt.Println("REGULAR SERVER")

	server, err := servers.NewServer(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %v", err.Error())
	}

	err = os.Chdir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	var defaultHost = "127.0.0.1"
	var defaultPort = 12344
	if server.Config.Defaults != nil {
		if server.Config.Defaults.Host != nil {
			defaultHost = *server.Config.Defaults.Host
		}
		if server.Config.Defaults.Port != nil {
			defaultPort = *server.Config.Defaults.Port
		}
	}

	// FlagSet for parsing the arguments
	argCmd := flag.NewFlagSet("args", flag.ExitOnError)

	// Predefined flags
	host := argCmd.String("host", defaultHost, "Host to listen on")
	port := argCmd.Int("port", defaultPort, "Port to listen on")
	debug := argCmd.Bool("debug", false, "Debug")

	// Placeholder for dynamic args
	parsedArgs := make(map[string]*string)

	finalArgs := map[string]string{}

	if len(server.Config.Args) > 0 {

		for key, value := range server.Config.Args {
			if strings.ContainsAny(key, " /\\,.!@#$%^&*()-+=|]{}\"`;") {
				return nil, fmt.Errorf("invalid key: '%s'", key)
			}

			var defaultVal = ""
			if value.Default != nil {
				defaultVal = *value.Default
			}
			parsedArgs[key] = argCmd.String(key, defaultVal, value.Description)
		}
		// Parse the flags
		if err := argCmd.Parse(os.Args[3:]); err != nil {
			return nil, fmt.Errorf("failed to parse args: %s. `%s`", err.Error(), os.Args[3:])
		}

		// Build final args map
		for key := range parsedArgs {
			if parsedArgs[key] == nil || (*parsedArgs[key] == "" && server.Config.Args[key].Default == nil) {
				return nil, fmt.Errorf("missing value: '%s': %s", key, server.Config.Args[key].Description)
			}

			finalArgs[key] = *parsedArgs[key]
		}
	} else {
		// Parse the flags
		if err := argCmd.Parse(os.Args[3:]); err != nil {
			return nil, fmt.Errorf("failed to parse args: %v", err.Error())
		}
	}

	hostIp := net.ProcessHost(*host)

	address := fmt.Sprintf("%s:%v", hostIp, *port)

	server.Address = address
	server.Args = finalArgs
	server.Debug = *debug

	return server, nil

}
