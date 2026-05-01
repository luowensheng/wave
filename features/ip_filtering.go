package features

import "net/http"

// IPFiltering is the capability of allowing or denying inbound requests by
// client IP. It is a struct of capability functions; the orchestrator wires
// concrete closures backed by infra/ipfilter at startup.
type IPFiltering struct {
	CheckRequest func(r *http.Request) (allowed bool, clientIP string)
	IsAllowed    func(ip string) bool
	GetClientIP  func(r *http.Request) string
}
