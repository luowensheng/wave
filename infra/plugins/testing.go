package plugins

// InjectForTest installs a Client into the registry under the given name.
// Test-only helper — keeps the registry's `clients` map private while
// still letting integration tests wire fakes.
func InjectForTest(r *Registry, name string, c Client) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clients == nil {
		r.clients = make(map[string]Client)
	}
	r.clients[name] = c
}
