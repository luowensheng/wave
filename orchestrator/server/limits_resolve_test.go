package servers

import (
	"strings"
	"testing"
)

func TestResolveLimitsHappyPath(t *testing.T) {
	registry := map[string]*LimitEntry{
		"size_5mb":    {Case: CaseBodyTooLarge, MaxSize: "5MB"},
		"rate_100":    {Case: CaseRateLimited, RPS: 100},
		"json_inputs": {Case: CaseInvalidInputs},
	}
	r := &Route{Limits: []string{"size_5mb", "rate_100", "json_inputs"}}
	if err := r.resolveLimits(registry); err != nil {
		t.Fatal(err)
	}
	if got := len(r.resolvedLimits); got != 3 {
		t.Fatalf("resolved %d entries, want 3", got)
	}
	if r.resolvedLimits[CaseBodyTooLarge].MaxSize != "5MB" {
		t.Errorf("body_too_large not bound correctly: %+v", r.resolvedLimits[CaseBodyTooLarge])
	}
	if r.resolvedLimits[CaseRateLimited].RPS != 100 {
		t.Errorf("rate_limited not bound correctly: %+v", r.resolvedLimits[CaseRateLimited])
	}
	// findLimit should agree.
	if r.findLimit(CaseInvalidInputs) == nil {
		t.Error("findLimit(invalid_inputs) returned nil")
	}
	if r.findLimit(CaseError) != nil {
		t.Error("findLimit(error) should be nil — not referenced")
	}
}

func TestResolveLimitsUnknownNameErrors(t *testing.T) {
	registry := map[string]*LimitEntry{
		"size_5mb": {Case: CaseBodyTooLarge, MaxSize: "5MB"},
	}
	r := &Route{Limits: []string{"size_5mb", "no_such_thing"}}
	err := r.resolveLimits(registry)
	if err == nil || !strings.Contains(err.Error(), `"no_such_thing"`) {
		t.Errorf("expected unknown-limit error, got %v", err)
	}
}

func TestResolveLimitsLastWinsOnCaseCollision(t *testing.T) {
	// rate_100 and tight_quota both bind CaseRateLimited. The route
	// references rate_100 first, then tight_quota — tight_quota wins.
	registry := map[string]*LimitEntry{
		"rate_100":    {Case: CaseRateLimited, RPS: 100},
		"tight_quota": {Case: CaseRateLimited, RPS: 5},
	}
	r := &Route{Limits: []string{"rate_100", "tight_quota"}}
	if err := r.resolveLimits(registry); err != nil {
		t.Fatal(err)
	}
	if got := r.findLimit(CaseRateLimited).RPS; got != 5 {
		t.Errorf("expected later entry (RPS=5) to win, got RPS=%v", got)
	}

	// And in the reverse order, rate_100 wins.
	r2 := &Route{Limits: []string{"tight_quota", "rate_100"}}
	if err := r2.resolveLimits(registry); err != nil {
		t.Fatal(err)
	}
	if got := r2.findLimit(CaseRateLimited).RPS; got != 100 {
		t.Errorf("expected later entry (RPS=100) to win, got RPS=%v", got)
	}
}

func TestResolveLimitsEmptyListIsNoop(t *testing.T) {
	r := &Route{}
	if err := r.resolveLimits(nil); err != nil {
		t.Fatal(err)
	}
	if r.resolvedLimits == nil {
		t.Error("resolvedLimits should be non-nil (empty map)")
	}
	if r.findLimit(CaseError) != nil {
		t.Error("findLimit on empty resolved map should return nil")
	}
}

func TestResolveLimitsRejectsEmptyCase(t *testing.T) {
	registry := map[string]*LimitEntry{
		"oops": {Case: ""}, // misconfigured entry
	}
	r := &Route{Limits: []string{"oops"}}
	err := r.resolveLimits(registry)
	if err == nil || !strings.Contains(err.Error(), "missing case") {
		t.Errorf("expected missing-case error, got %v", err)
	}
}

func TestResolveLimitsRejectsUnknownCase(t *testing.T) {
	registry := map[string]*LimitEntry{
		"weird": {Case: "not_a_real_case"},
	}
	r := &Route{Limits: []string{"weird"}}
	err := r.resolveLimits(registry)
	if err == nil || !strings.Contains(err.Error(), "unknown case") {
		t.Errorf("expected unknown-case error, got %v", err)
	}
}

func TestIsKnownCase(t *testing.T) {
	for _, c := range []string{
		CaseBodyTooLarge, CaseInvalidInputs, CaseRateLimited,
		CaseCircuitOpen, CaseUnauthenticated, CaseForbidden,
		CaseMissingSignature, CaseError,
	} {
		if !isKnownCase(c) {
			t.Errorf("isKnownCase(%q) = false, want true", c)
		}
	}
	for _, bad := range []string{"", "bogus", "RATE_LIMITED" /* case-sensitive */} {
		if isKnownCase(bad) {
			t.Errorf("isKnownCase(%q) = true, want false", bad)
		}
	}
}
