package main

import (
	"context"
	"database/sql"
	"github.com/luowensheng/wave/infra/migrate"
	"github.com/luowensheng/wave/infra/outbox"
	"github.com/luowensheng/wave/infra/net"
	"github.com/luowensheng/wave/infra/wavetest"
	"github.com/luowensheng/wave/orchestrator/scaffold"
	"github.com/luowensheng/wave/orchestrator/server"
	"github.com/luowensheng/wave/orchestrator/studio"

	log "github.com/luowensheng/wave/infra/logger"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/yaml.v3"
)

// version is overridden at build time via:
//   go build -ldflags "-X main.version=v0.1.0 -X main.commit=$(git rev-parse --short HEAD)" ./orchestrator
// Defaults to "dev" so local builds are obvious in logs and bug reports.
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve", "run":
		serve()
	case "serve-live":
		servelive()
	case "validate":
		validate()
	case "fmt":
		fmtCmd()
	case "test":
		testCmd()
	case "init":
		initCmd()
	case "routes":
		routesCmd()
	case "migrate":
		migrateCmd()
	case "doctor":
		doctorCmd()
	case "outbox":
		outboxCmd()
	case "studio":
		studioCmd()
	case "completion":
		completionCmd()
	case "version", "--version", "-v":
		fmt.Printf("wave %s (commit: %s)\n", version, commit)
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", os.Args[1])
		printHelp()
		os.Exit(2)
	}
}

// printHelp emits a top-level usage banner. Kept brief — long usage
// per-subcommand prints from the individual command functions.
func printHelp() {
	fmt.Fprintln(os.Stderr, `wave — declarative HTTP server framework

USAGE:
  wave <command> [arguments...]

COMMANDS:
  serve <file.yaml>                 Run a server
  serve-live <file.yaml>            Run a server, hot-reload on file change
  validate <file.yaml>              Boot-time config check (no server)
  test <suite.test.yaml>            Run a functional test suite
  fmt <file.yaml>                   Canonicalize YAML formatting
  routes <file.yaml>                Print the route table (table | json)
  doctor <file.yaml>                Pre-flight diagnostics (live connectivity)
  init <template> <dir>             Scaffold a starter project
  migrate up|down                   Apply / roll back SQLite migrations
  outbox list|dlq|replay            Inspect and operate on the outbox
  studio                            Multi-project web UI
  completion bash|zsh|fish          Print shell completion script
  version                           Print build version
  help                              Show this message

EXAMPLES:
  wave serve server.yaml --port 8080
  wave validate server.yaml
  wave test server.test.yaml
  wave init api ./my-project
  wave doctor server.yaml --json

DOCS:
  https://luowensheng.github.io/wave/
  https://github.com/luowensheng/wave`)
}

// outboxCmd inspects and operates on the durable outbound webhook
// outbox. Supports:
//
//	wave outbox list   --db <path>            (live + DLQ counts)
//	wave outbox dlq    --db <path>            (recent dead-lettered)
//	wave outbox replay --db <path> --id N     (single)
//	wave outbox replay --db <path> --all      (drain DLQ)
func outboxCmd() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: wave outbox list|dlq|replay --db <path> [--id N | --all]")
	}
	sub := os.Args[2]
	fs := flag.NewFlagSet("outbox", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to SQLite outbox DB")
	id := fs.Int64("id", 0, "single DLQ entry id (replay only)")
	all := fs.Bool("all", false, "replay every DLQ entry")
	if err := fs.Parse(os.Args[3:]); err != nil {
		log.Fatal(err)
	}
	if *dbPath == "" {
		log.Fatal("outbox: --db is required")
	}
	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("outbox: open: %v", err)
	}
	defer db.Close()
	store, err := outbox.NewSQLiteStore(db)
	if err != nil {
		log.Fatalf("outbox: store: %v", err)
	}
	ctx := context.Background()
	switch sub {
	case "list":
		live, _ := store.Pending(ctx)
		dlq, _ := store.DLQList(ctx, 1000)
		fmt.Printf("live queue: %d\nDLQ:        %d\n", live, len(dlq))
	case "dlq":
		entries, err := store.DLQList(ctx, 100)
		if err != nil {
			log.Fatalf("outbox dlq: %v", err)
		}
		if len(entries) == 0 {
			fmt.Println("DLQ empty")
			return
		}
		for _, d := range entries {
			fmt.Printf("%-6d  %-40s  attempts=%d  err=%q\n", d.ID, d.URL, d.Attempts, d.LastError)
		}
	case "replay":
		if *all {
			n, err := store.ReplayAll(ctx)
			if err != nil {
				log.Fatalf("outbox replay --all: %v", err)
			}
			fmt.Printf("replayed %d entries\n", n)
			return
		}
		if *id == 0 {
			log.Fatal("outbox replay: --id N or --all required")
		}
		newID, err := store.Replay(ctx, *id)
		if err != nil {
			log.Fatalf("outbox replay %d: %v", *id, err)
		}
		fmt.Printf("replayed dlq=%d → live=%d\n", *id, newID)
	default:
		log.Fatalf("outbox: unknown sub %q (want list|dlq|replay)", sub)
	}
}

// doctorCmd runs validate + live connectivity checks (HTTP plugins,
// OIDC discovery, SQLite ping, referenced files) against a config and
// prints a report. Exits non-zero when any check fails.
//
//	wave doctor <file.yaml>            (human-readable table)
//	wave doctor <file.yaml> --json     (machine-readable, suitable for CI)
func doctorCmd() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: wave doctor <path/to/server.yaml> [--json]")
	}
	jsonOut := false
	for _, a := range os.Args[3:] {
		if a == "--json" {
			jsonOut = true
		}
	}
	path, err := filepath.Abs(os.Args[2])
	if err != nil {
		log.Fatal(err.Error())
	}
	srv, err := servers.NewServer(path)
	if err != nil {
		log.Fatalf("doctor: %v", err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results, failures := srv.RunDoctor(ctx)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"checks":   results,
			"failures": failures,
			"ok":       failures == 0,
		})
		if failures > 0 {
			os.Exit(1)
		}
		return
	}

	icon := func(status string) string {
		switch status {
		case "ok":
			return "OK   "
		case "warn":
			return "WARN "
		case "fail":
			return "FAIL "
		default:
			return "?    "
		}
	}
	for _, r := range results {
		fmt.Printf("%s %-30s  %s\n", icon(r.Status), r.Name, r.Message)
	}
	fmt.Printf("\n%d checks, %d failures\n", len(results), failures)
	if failures > 0 {
		os.Exit(1)
	}
}

// migrateCmd applies or reverses SQLite migrations stored as numbered
// .up.sql / .down.sql files in a directory.
//
//	wave migrate up   --db ./data.db --dir ./migrations
//	wave migrate down --db ./data.db --dir ./migrations
//
// Idempotent: applied state lives in the `_wave_migrations`
// table inside the same DB.
func migrateCmd() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: wave migrate up|down --db <path> --dir <migrations-dir>")
	}
	direction := os.Args[2]
	if direction != "up" && direction != "down" {
		log.Fatalf("migrate: unknown direction %q (want up|down)", direction)
	}
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to SQLite database file")
	dir := fs.String("dir", "", "migrations directory")
	if err := fs.Parse(os.Args[3:]); err != nil {
		log.Fatal(err)
	}
	if *dbPath == "" || *dir == "" {
		log.Fatal("migrate: --db and --dir are required")
	}
	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("migrate: open db: %v", err)
	}
	defer db.Close()

	switch direction {
	case "up":
		ran, err := migrate.Up(db, *dir)
		if err != nil {
			log.Fatalf("migrate up: %v", err)
		}
		if len(ran) == 0 {
			fmt.Println("nothing to migrate")
			return
		}
		for _, m := range ran {
			fmt.Printf("applied %04d_%s\n", m.Version, m.Name)
		}
	case "down":
		m, err := migrate.Down(db, *dir)
		if err != nil {
			log.Fatalf("migrate down: %v", err)
		}
		if m == nil {
			fmt.Println("nothing to roll back")
			return
		}
		fmt.Printf("rolled back %04d_%s\n", m.Version, m.Name)
	}
}

// routesCmd prints the route table of a config without booting the
// server. Useful for CI checks ("does this config still expose the
// expected endpoints?") and quick inspection. Output supports two
// formats: --format=table (default, human-readable) and --format=json
// (machine-readable, identical shape to the /openapi.json route entries).
func routesCmd() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: wave routes <path/to/server.yaml> [--format=table|json]")
	}
	format := "table"
	for _, a := range os.Args[3:] {
		if strings.HasPrefix(a, "--format=") {
			format = strings.TrimPrefix(a, "--format=")
		}
	}
	path, err := filepath.Abs(os.Args[2])
	if err != nil {
		log.Fatal(err.Error())
	}
	srv, err := servers.NewServer(path)
	if err != nil {
		log.Fatalf("routes: %v", err.Error())
	}
	// Materialize the raw routes (loadConfig doesn't do this — Start does).
	rows, err := srv.RouteSummaries()
	if err != nil {
		log.Fatalf("routes: %v", err.Error())
	}
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
	default:
		fmt.Printf("%-7s  %-40s  %-15s  %s\n", "METHOD", "PATH", "TYPE", "AUTH")
		for _, r := range rows {
			method := r.Method
			if method == "" {
				method = "GET"
			}
			fmt.Printf("%-7s  %-40s  %-15s  %s\n",
				method, r.Path, r.Type, strings.Join(r.Auth, ","))
		}
		fmt.Printf("\n%d routes\n", len(rows))
	}
}

// initCmd writes a starter project to disk. Usage:
//
//	wave init <template> <dir> [--force]
//	wave init list
func initCmd() {
	if len(os.Args) >= 3 && os.Args[2] == "list" {
		fmt.Println("Available templates:")
		for _, t := range scaffold.All() {
			fmt.Printf("  %-16s %s\n", t.Name, t.Description)
		}
		return
	}
	if len(os.Args) < 4 {
		log.Fatal("Usage: wave init <template> <dir> [--force]\n       wave init list")
	}
	name := os.Args[2]
	dir := os.Args[3]
	force := false
	for _, a := range os.Args[4:] {
		if a == "--force" || a == "-f" {
			force = true
		}
	}
	tpl, ok := scaffold.Get(name)
	if !ok {
		log.Fatalf("unknown template %q (try `wave init list`)", name)
	}
	if err := scaffold.Render(tpl, dir, force); err != nil {
		log.Fatalf("init: %v", err.Error())
	}
	fmt.Printf("scaffolded %s into %s\n", name, dir)
}

// validate loads a config file and checks that every route has a known
// type, every plugin route names a registered plugin, every stream-publish
// route names a registered connection, and that every plugin/connection
// validates. Exits 0 on success, 1 on first error — suitable for CI.
func validate() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: wave validate <path/to/server.yaml>")
	}
	path, err := filepath.Abs(os.Args[2])
	if err != nil {
		log.Fatal(err.Error())
	}
	srv, err := servers.NewServer(path)
	if err != nil {
		log.Fatalf("validate: %v", err.Error())
	}
	if err := srv.ValidateConfig(); err != nil {
		log.Fatalf("validate: %v", err.Error())
	}
	fmt.Println("ok")
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

// testCmd runs a wavetest YAML suite against an in-process Wave
// server (no port binding). Suitable for CI gates and pre-commit
// hooks.
//
//	wave test server.test.yaml
//	wave test server.test.yaml --json     (machine-readable summary)
//	wave test server.test.yaml --verbose  (per-case timing + status)
func testCmd() {
	// Use fmt.Fprintln(os.Stderr, ...) + os.Exit(1) instead of
	// log.Fatal: Wave's logger redirects stdlib `log` to a file at
	// boot, so log.Fatal silently swallows the message at the
	// terminal — terrible UX for a CLI error.
	die := func(msg string) {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(2)
	}
	if len(os.Args) < 3 {
		die("Usage: wave test <suite.test.yaml> [--json | --verbose]")
	}
	jsonOut := false
	verbose := false
	suite := ""
	for _, a := range os.Args[2:] {
		switch a {
		case "--json":
			jsonOut = true
		case "-v", "--verbose":
			verbose = true
		default:
			if suite == "" {
				suite = a
			}
		}
	}
	if suite == "" {
		die("test: a suite path is required (e.g. server.test.yaml)")
	}
	absSuite, err := filepath.Abs(suite)
	if err != nil {
		die("test: " + err.Error())
	}
	// `wave test` is the only command we run that needs the suite's
	// directory as cwd so server.yaml relative paths resolve.
	if err := os.Chdir(filepath.Dir(absSuite)); err != nil {
		die("test: chdir: " + err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Default mode silences server boot prints + access logs (via
	// fd-level dup2) so the pass/fail report and JSON envelope are
	// clean. --verbose keeps everything visible.
	summary, err := wavetest.RunFileWithOptions(ctx, absSuite, wavetest.Options{
		Quiet: !verbose,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "test: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(summary)
		if !summary.OK {
			os.Exit(1)
		}
		return
	}

	// Human-readable. One line per case; per-case errors inline.
	for _, r := range summary.Results {
		icon := "PASS"
		if !r.Passed {
			icon = "FAIL"
		}
		fmt.Printf("  %s [%s] %s (%d, %s)\n",
			icon, r.Phase, r.Name, r.Status, r.Duration.Round(time.Millisecond))
		for _, e := range r.Errors {
			fmt.Printf("       %s\n", e)
		}
	}
	fmt.Println()
	fmt.Printf("  %d passed, %d failed, %.2fs\n",
		summary.Passed, summary.Failed, summary.Duration)
	if !summary.OK {
		os.Exit(1)
	}
}

// fmtCmd canonicalizes the indentation and structure of one or more
// server.yaml files via a yaml.v3 round-trip. Useful as a pre-commit
// hook to cut PR review noise from indentation churn. By default
// rewrites the file in place; --check exits non-zero if formatting
// would change anything (CI mode).
//
//	wave fmt server.yaml
//	wave fmt server.yaml --check     (exits 1 if not formatted)
//	wave fmt server.yaml --stdout    (print, don't rewrite)
func fmtCmd() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: wave fmt <file.yaml> [--check | --stdout]")
	}
	check := false
	stdout := false
	files := []string{}
	for _, a := range os.Args[2:] {
		switch a {
		case "--check":
			check = true
		case "--stdout":
			stdout = true
		default:
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		log.Fatal("fmt: at least one file is required")
	}

	changed := 0
	for _, path := range files {
		formatted, original, err := formatYAMLFile(path)
		if err != nil {
			log.Fatalf("fmt %s: %v", path, err)
		}
		if string(formatted) == string(original) {
			if !check && !stdout {
				fmt.Printf("unchanged %s\n", path)
			}
			continue
		}
		changed++
		switch {
		case check:
			fmt.Fprintf(os.Stderr, "%s: would be reformatted\n", path)
		case stdout:
			_, _ = os.Stdout.Write(formatted)
		default:
			if err := os.WriteFile(path, formatted, 0o644); err != nil {
				log.Fatalf("fmt %s: write: %v", path, err)
			}
			fmt.Printf("reformatted %s\n", path)
		}
	}

	if check && changed > 0 {
		os.Exit(1)
	}
}

// formatYAMLFile reads `path`, round-trips through yaml.v3 (which
// canonicalizes indentation and quoting), and returns (new, old, err).
// Comments and key order are preserved by yaml.v3's Node API.
func formatYAMLFile(path string) (formatted, original []byte, err error) {
	original, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(original, &node); err != nil {
		return nil, nil, fmt.Errorf("yaml parse: %w", err)
	}
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return nil, nil, fmt.Errorf("yaml encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, nil, fmt.Errorf("yaml close: %w", err)
	}
	return []byte(buf.String()), original, nil
}

// completionCmd prints a shell completion script for bash, zsh, or
// fish. Top-level subcommand list is hard-coded; per-command flags
// are not completed (kept simple — Wave's CLI is small).
//
//	wave completion bash >> ~/.bash_completion
//	wave completion zsh  > "${fpath[1]}/_wave"
//	wave completion fish > ~/.config/fish/completions/wave.fish
func completionCmd() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: wave completion bash|zsh|fish")
	}
	switch os.Args[2] {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		log.Fatalf("completion: unknown shell %q (want bash|zsh|fish)", os.Args[2])
	}
}

const bashCompletion = `# wave shell completion (bash) — install with:
#   wave completion bash >> ~/.bash_completion
_wave_complete() {
  local cur prev opts
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  if [ "$COMP_CWORD" -eq 1 ]; then
    opts="serve serve-live validate fmt routes doctor init migrate outbox studio completion version help"
    COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
    return 0
  fi

  case "${COMP_WORDS[1]}" in
    serve|serve-live|validate|fmt|routes|doctor)
      COMPREPLY=( $(compgen -f -X '!*.@(yaml|yml)' -- "${cur}") )
      ;;
    migrate)
      [ "$COMP_CWORD" -eq 2 ] && COMPREPLY=( $(compgen -W "up down" -- "${cur}") )
      ;;
    outbox)
      [ "$COMP_CWORD" -eq 2 ] && COMPREPLY=( $(compgen -W "list dlq replay" -- "${cur}") )
      ;;
    init)
      [ "$COMP_CWORD" -eq 2 ] && COMPREPLY=( $(compgen -W "api spa internal-tool plugin-starter streaming oidc-api graphql list" -- "${cur}") )
      ;;
    completion)
      [ "$COMP_CWORD" -eq 2 ] && COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") )
      ;;
  esac
}
complete -F _wave_complete wave
`

const zshCompletion = `#compdef wave
# wave shell completion (zsh) — install with:
#   wave completion zsh > "${fpath[1]}/_wave"
_wave() {
  local -a subcmds
  subcmds=(
    'serve:Run a server'
    'serve-live:Run with hot-reload'
    'validate:Boot-time config check'
    'fmt:Canonicalize YAML'
    'routes:Print the route table'
    'doctor:Pre-flight diagnostics'
    'init:Scaffold a starter project'
    'migrate:Apply / roll back migrations'
    'outbox:Inspect the outbox'
    'studio:Multi-project web UI'
    'completion:Print shell completion'
    'version:Print build version'
    'help:Show usage'
  )
  if (( CURRENT == 2 )); then
    _describe 'command' subcmds
    return
  fi
  case "$words[2]" in
    serve|serve-live|validate|fmt|routes|doctor) _files -g '*.yaml *.yml' ;;
    migrate)    (( CURRENT == 3 )) && _values 'direction' up down ;;
    outbox)     (( CURRENT == 3 )) && _values 'sub' list dlq replay ;;
    init)       (( CURRENT == 3 )) && _values 'template' api spa internal-tool plugin-starter streaming oidc-api graphql list ;;
    completion) (( CURRENT == 3 )) && _values 'shell' bash zsh fish ;;
  esac
}
_wave "$@"
`

const fishCompletion = `# wave shell completion (fish) — install with:
#   wave completion fish > ~/.config/fish/completions/wave.fish

complete -c wave -f

# Top-level subcommands
complete -c wave -n __fish_use_subcommand -a serve       -d 'Run a server'
complete -c wave -n __fish_use_subcommand -a serve-live  -d 'Run with hot-reload'
complete -c wave -n __fish_use_subcommand -a validate    -d 'Boot-time config check'
complete -c wave -n __fish_use_subcommand -a fmt         -d 'Canonicalize YAML'
complete -c wave -n __fish_use_subcommand -a routes      -d 'Print the route table'
complete -c wave -n __fish_use_subcommand -a doctor      -d 'Pre-flight diagnostics'
complete -c wave -n __fish_use_subcommand -a init        -d 'Scaffold a starter project'
complete -c wave -n __fish_use_subcommand -a migrate     -d 'Apply / roll back migrations'
complete -c wave -n __fish_use_subcommand -a outbox      -d 'Inspect the outbox'
complete -c wave -n __fish_use_subcommand -a studio      -d 'Multi-project web UI'
complete -c wave -n __fish_use_subcommand -a completion  -d 'Print shell completion'
complete -c wave -n __fish_use_subcommand -a version     -d 'Print build version'
complete -c wave -n __fish_use_subcommand -a help        -d 'Show usage'

# yaml file completion for the path-taking commands
complete -c wave -n '__fish_seen_subcommand_from serve serve-live validate fmt routes doctor' -a '(__fish_complete_path)' -k

# subsubcommands
complete -c wave -n '__fish_seen_subcommand_from migrate'    -a 'up down'
complete -c wave -n '__fish_seen_subcommand_from outbox'     -a 'list dlq replay'
complete -c wave -n '__fish_seen_subcommand_from init'       -a 'api spa internal-tool plugin-starter streaming oidc-api graphql list'
complete -c wave -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
`

// studioCmd boots the multi-project Studio web UI.
//
//	wave studio [--host 127.0.0.1] [--port 8081] [--data-dir ~/.wave] [--no-browser]
func studioCmd() {
	fs := flag.NewFlagSet("studio", flag.ExitOnError)
	host := fs.String("host", "127.0.0.1", "bind host (127.0.0.1 only by default)")
	port := fs.Int("port", 8081, "studio HTTP port")
	dataDir := fs.String("data-dir", "~/.wave", "studio state directory")
	noBrowser := fs.Bool("no-browser", false, "do not auto-open the URL in a browser")
	if err := fs.Parse(os.Args[2:]); err != nil {
		log.Fatal(err)
	}
	if err := studio.Serve(*host, *port, *dataDir, !*noBrowser); err != nil {
		log.Fatalf("studio: %v", err)
	}
}
