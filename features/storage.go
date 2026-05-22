package features

import "github.com/luowensheng/wave/domain"

// Storage is the capability of persisting and querying domain data
// against a named storage backend (sqlite, filesystem, ...). The
// orchestrator wires concrete closures backed by infra/sqlite and
// infra/filesystem at startup.
type Storage struct {
	Execute func(refName, command string, data StorageInput) (any, error)
	Get     func(refName string) (StorageRef, bool)
}

// StorageInput is the shape of input the storage feature accepts. The
// concrete request-parsing implementation lives in io/http/contentloader.
type StorageInput interface {
	GetValue(name string) (any, error)
	GetFile(name string) (*domain.File, error)
	GetValues() map[string]any
}

// StorageRef is an opaque handle for a configured backend.
type StorageRef interface {
	Execute(command string, data StorageInput) (any, error)
}
