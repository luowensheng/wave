package features

// make_storage.go wires the concrete storage registry into the
// features.Storage capability struct.

import (
	capfeatures "github.com/luowensheng/wave/features"
	storagefeature "github.com/luowensheng/wave/orchestrator/features/storage"
	"github.com/luowensheng/wave/io/http/contentloader"
)

// MakeStorage returns a populated features.Storage by closing over
// the storage registry initialized by storage.InitStorage.
// Must be called after storage.InitStorage.
func MakeStorage() capfeatures.Storage {
	return capfeatures.Storage{
		Get: func(refName string) (capfeatures.StorageRef, bool) {
			ref, ok := storagefeature.GetFromStorage(refName)
			if !ok {
				return nil, false
			}
			return &storageRefAdapter{inner: ref}, true
		},
		Execute: func(refName, command string, data capfeatures.StorageInput) (any, error) {
			ref, ok := storagefeature.GetFromStorage(refName)
			if !ok {
				return nil, nil
			}
			// contentloader.DataLoader satisfies the concrete Execute signature.
			dl, _ := data.(*contentloader.DataLoader)
			return ref.Execute(command, dl)
		},
	}
}

// storageRefAdapter bridges orchestrator/features/storage.StorageRef to
// the features.StorageRef interface.
type storageRefAdapter struct {
	inner storagefeature.StorageRef
}

func (a *storageRefAdapter) Execute(command string, data capfeatures.StorageInput) (any, error) {
	dl, _ := data.(*contentloader.DataLoader)
	return a.inner.Execute(command, dl)
}
