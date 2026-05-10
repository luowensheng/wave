package servers

import (
	"net/http"

	infrahttp "wave/infra/http"
)

// limitFor returns (entry, onFail) for the named case. onFail is nil
// when no entry exists OR when the entry has no on_fail action — in
// which case the caller should use its built-in default response.
func (s *Server) limitFor(route *Route, caseName string) (*LimitEntry, http.HandlerFunc) {
	entry := route.findLimit(caseName)
	if entry == nil {
		return nil, nil
	}
	r, err := compileFailAction(entry.OnFail, defaultStatusFor(caseName), nil, s.mux)
	if err != nil {
		// Misconfigured action — fall back to the case's natural default.
		return entry, nil
	}
	return entry, r.asHandler()
}

func defaultStatusFor(caseName string) int {
	switch caseName {
	case CaseBodyTooLarge:
		return http.StatusRequestEntityTooLarge
	case CaseInvalidInputs:
		return http.StatusBadRequest
	case CaseRateLimited:
		return http.StatusTooManyRequests
	case CaseCircuitOpen:
		return http.StatusServiceUnavailable
	case CaseUnauthenticated:
		return http.StatusUnauthorized
	case CaseForbidden:
		return http.StatusForbidden
	case CaseMissingSignature:
		return http.StatusUnauthorized
	case CaseError:
		return http.StatusInternalServerError
	}
	return http.StatusInternalServerError
}

// resolveBodyLimit returns the configured body-limit middleware config
// for a route, or nil when `limits[case=body_too_large]` is absent.
func (s *Server) resolveBodyLimit(route *Route) (*infrahttp.BodyLimitConfig, error) {
	entry, onFail := s.limitFor(route, CaseBodyTooLarge)
	if entry == nil {
		return nil, nil
	}
	max, err := infrahttp.ParseBytesString(entry.MaxSize)
	if err != nil {
		return nil, err
	}
	return &infrahttp.BodyLimitConfig{
		MaxBytes: max,
		OnFail:   onFail,
	}, nil
}
