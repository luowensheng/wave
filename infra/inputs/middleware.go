package inputs

import (
	"context"
	"encoding/json"
	"net/http"
)

// Middleware parses + validates declared inputs, stashes the result on
// context, and short-circuits with a single 400 + JSON error envelope
// listing every problem when validation fails.
//
// Composes naturally with the existing redirect_on_error middleware:
// callers who prefer to redirect can list 400 in their status_codes.
func Middleware(set *SpecSet) func(http.Handler) http.Handler {
	return MiddlewareWithFail(set, nil)
}

// MiddlewareWithFail behaves like Middleware but lets the caller swap
// the 400-with-JSON rejection for a custom handler. Validation issues
// are exposed via context (IssuesFromContext) so the renderer can
// log them or surface them in a template.
//
// Used by the orchestrator's `limits:` block to share a unified
// renderer (redirect / template_inline / template_file / route_path)
// across every middleware.
func MiddlewareWithFail(set *SpecSet, onFail http.HandlerFunc) func(http.Handler) http.Handler {
	if set == nil || len(set.List) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res := set.Parse(r)
			if len(res.Issues) > 0 {
				if onFail != nil {
					r = r.WithContext(WithIssues(r.Context(), res.Issues))
					onFail(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":  "input validation failed",
					"issues": res.Issues,
				})
				return
			}
			r = r.WithContext(WithValues(r.Context(), res.Values))
			next.ServeHTTP(w, r)
		})
	}
}

// issueCtxKey is a separate context key from values so a renderer can
// distinguish "validated values" from "validation problems".
type issueCtxKey struct{}

func WithIssues(ctx context.Context, is []Issue) context.Context {
	return context.WithValue(ctx, issueCtxKey{}, is)
}

func IssuesFromContext(ctx context.Context) []Issue {
	if v, ok := ctx.Value(issueCtxKey{}).([]Issue); ok {
		return v
	}
	return nil
}
