// Package servers — storage_plugin wires plugin-backed storage backends
// into the server boot. Built-in storage (Config.Storage) takes
// precedence; plugin-kind=storage entries fill in for any other source
// names referenced by storage_access routes.
package servers

import (
	"fmt"

	storageaccess "wave/usecases/storage_access"
)

// validateStorageRefs walks every storage_access route and confirms
// its `source` resolves to either a built-in storage backend or a
// kind=storage plugin. Fails the boot if any route points at an
// unknown source so we never serve a 500 at request time.
func (s *Server) validateStorageRefs() error {
	if storageaccess.GetStorageFn == nil {
		// No storage_access routes will work without a wired lookup;
		// this is normal for static-only servers, just skip validation.
		return nil
	}
	for _, r := range s.Config.Routes {
		if r == nil || r.Type != "storage-access" || r.StorageAccessConfig == nil {
			continue
		}
		src := r.StorageAccessConfig.Source
		if src == "" {
			return fmt.Errorf("route %q: storage-access source is empty", r.Path)
		}
		if _, ok := storageaccess.GetStorageFn(src); !ok {
			return fmt.Errorf(
				"route %q: storage-access source %q does not resolve to a built-in storage or kind=storage plugin",
				r.Path, src,
			)
		}
	}
	return nil
}
