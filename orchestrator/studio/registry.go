// Package studio implements the multi-project Studio UI: a local web
// app that lets users register, supervise, inspect, and exercise
// multiple wave projects from one place. Phase 6.
//
// registry.go owns the persisted project list at
// $DATA_DIR/projects.json. Atomic writes via temp file + rename.
package studio

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Project is one registered wave project.
type Project struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	ConfigFile string    `json:"config_file"`
	AddedAt    time.Time `json:"added_at"`
}

// ConfigPath returns the absolute path of the project's config file.
func (p *Project) ConfigPath() string {
	return filepath.Join(p.Path, p.ConfigFile)
}

// Registry is the persisted list of projects.
type Registry struct {
	Projects []*Project `json:"projects"`

	dataDir string
	mu      sync.RWMutex
}

// LoadRegistry reads the registry from dataDir, creating an empty one
// if the file does not exist.
func LoadRegistry(dataDir string) (*Registry, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	r := &Registry{dataDir: dataDir, Projects: []*Project{}}
	path := filepath.Join(dataDir, "projects.json")
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes) == 0 {
		return r, nil
	}
	if err := json.Unmarshal(bytes, r); err != nil {
		return nil, fmt.Errorf("registry: parse: %w", err)
	}
	if r.Projects == nil {
		r.Projects = []*Project{}
	}
	return r, nil
}

// Save atomically writes the registry to disk via temp + rename.
func (r *Registry) Save() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.saveLocked()
}

func (r *Registry) saveLocked() error {
	path := filepath.Join(r.dataDir, "projects.json")
	tmp, err := os.CreateTemp(r.dataDir, "projects-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// Add registers an existing project. The directory must exist and
// contain configFile. Returns the new Project.
func (r *Registry) Add(path, configFile string) (*Project, error) {
	if configFile == "" {
		configFile = "server.yaml"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("registry: project path: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("registry: %s is not a directory", abs)
	}
	cfgPath := filepath.Join(abs, configFile)
	if _, err := os.Stat(cfgPath); err != nil {
		return nil, fmt.Errorf("registry: config file: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range r.Projects {
		if p.Path == abs && p.ConfigFile == configFile {
			return nil, fmt.Errorf("registry: project already registered (id=%s)", p.ID)
		}
	}

	name := deriveName(cfgPath, abs)
	id := stableID(abs, configFile)
	p := &Project{
		ID:         id,
		Name:       name,
		Path:       abs,
		ConfigFile: configFile,
		AddedAt:    time.Now().UTC(),
	}
	r.Projects = append(r.Projects, p)
	if err := r.saveLocked(); err != nil {
		// rollback
		r.Projects = r.Projects[:len(r.Projects)-1]
		return nil, err
	}
	return p, nil
}

// Remove unregisters by ID. Does not delete files on disk.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.Projects {
		if p.ID == id {
			r.Projects = append(r.Projects[:i], r.Projects[i+1:]...)
			return r.saveLocked()
		}
	}
	return fmt.Errorf("registry: project %q not found", id)
}

// Get returns the project for id and whether it exists.
func (r *Registry) Get(id string) (*Project, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.Projects {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

// List returns a snapshot of every project.
func (r *Registry) List() []*Project {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Project, len(r.Projects))
	copy(out, r.Projects)
	return out
}

// stableID derives a deterministic short ID from path + configFile so
// re-registering yields the same ID (idempotent across restarts).
func stableID(path, configFile string) string {
	h := sha1.Sum([]byte(path + "|" + configFile))
	return hex.EncodeToString(h[:8])
}

// deriveName picks a project name. Prefers `name:` from the YAML if
// present; otherwise falls back to the directory basename.
func deriveName(configPath, dir string) string {
	bytes, err := os.ReadFile(configPath)
	if err == nil {
		var probe struct {
			Name string `yaml:"name"`
		}
		if yaml.Unmarshal(bytes, &probe) == nil && probe.Name != "" {
			return probe.Name
		}
	}
	return filepath.Base(dir)
}
