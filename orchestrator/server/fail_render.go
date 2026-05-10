package servers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// failRenderer is the per-route, per-case http.Handler installed when
// a `limits[].on_fail` is configured. It encapsulates one of: redirect,
// inline template, file template, route delegation, or the natural
// JSON default for the case.
//
// The same shape powers every case — body_too_large, invalid_inputs,
// rate_limited, etc. — so wording (status, body, headers) lives in one
// place per route.
type failRenderer struct {
	action        *FailAction
	defaultStatus int
	defaultJSON   func(w http.ResponseWriter, r *http.Request) // built-in JSON envelope
	tplBody       []byte                                       // pre-loaded TemplateFile contents
	mux           *http.ServeMux                               // for RoutePath delegation
}

// compile builds a renderer from the user-supplied action. mux is
// used only when action.RoutePath delegates to a registered route.
func compileFailAction(
	action *FailAction,
	defaultStatus int,
	defaultJSON func(w http.ResponseWriter, r *http.Request),
	mux *http.ServeMux,
) (*failRenderer, error) {
	r := &failRenderer{
		action:        action,
		defaultStatus: defaultStatus,
		defaultJSON:   defaultJSON,
		mux:           mux,
	}
	if action != nil && action.TemplateFile != "" {
		body, err := os.ReadFile(action.TemplateFile)
		if err != nil {
			return nil, fmt.Errorf("template_file %q: %w", action.TemplateFile, err)
		}
		r.tplBody = body
	}
	return r, nil
}

// Render writes the configured response. Safe to call from any
// middleware that would otherwise emit its built-in error response.
func (f *failRenderer) Render(w http.ResponseWriter, r *http.Request) {
	a := f.action
	if a == nil {
		// No customization → use the case's natural JSON envelope.
		if f.defaultJSON != nil {
			f.defaultJSON(w, r)
			return
		}
		http.Error(w, http.StatusText(f.defaultStatus), f.defaultStatus)
		return
	}

	// Headers apply to every action variant.
	for k, v := range a.Headers {
		w.Header().Set(k, v)
	}

	switch {
	case a.RoutePath != "":
		// Internal dispatch: rebuild the request URL so the mux picks
		// the configured route. We DON'T set a status here — the
		// delegated handler is in charge of its own status.
		if f.mux == nil {
			http.Error(w, "fail-action route_path: mux unavailable", http.StatusInternalServerError)
			return
		}
		r2 := r.Clone(r.Context())
		u, err := url.Parse(a.RoutePath)
		if err != nil {
			http.Error(w, "fail-action route_path invalid", http.StatusInternalServerError)
			return
		}
		r2.URL = u
		r2.RequestURI = a.RoutePath
		f.mux.ServeHTTP(w, r2)
		return

	case a.Redirect != "":
		http.Redirect(w, r, a.Redirect, http.StatusFound)
		return

	case len(f.tplBody) > 0:
		ct := "text/html; charset=utf-8"
		if !looksHTML(f.tplBody) {
			ct = "text/plain; charset=utf-8"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(f.statusOr(f.defaultStatus))
		_, _ = w.Write(f.tplBody)
		return

	case a.TemplateInline != "":
		ct := "text/html; charset=utf-8"
		if !looksHTML([]byte(a.TemplateInline)) {
			ct = "text/plain; charset=utf-8"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(f.statusOr(f.defaultStatus))
		_, _ = w.Write([]byte(a.TemplateInline))
		return
	}

	// Action set but no body / redirect / route — emit the default
	// envelope, possibly with the user's status override.
	if a.Status != 0 && f.defaultJSON != nil {
		// Wrap the JSON path with the override status.
		writeStatusOnly(w, a.Status, "")
		return
	}
	if f.defaultJSON != nil {
		f.defaultJSON(w, r)
		return
	}
	http.Error(w, http.StatusText(f.defaultStatus), f.defaultStatus)
}

func (f *failRenderer) statusOr(def int) int {
	if f.action != nil && f.action.Status != 0 {
		return f.action.Status
	}
	return def
}

// looksHTML returns true when the body looks like HTML (starts with `<`
// after whitespace). Used to pick a sensible Content-Type for the
// inline / file template variants.
func looksHTML(b []byte) bool {
	for _, c := range b {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		return c == '<'
	}
	return false
}

// writeStatusOnly emits a tiny JSON envelope with just the status —
// helpful for the "user overrode status but provided no body" case.
func writeStatusOnly(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if msg == "" {
		msg = http.StatusText(status)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg, "status": status})
}

// asHandler wraps a renderer in an http.HandlerFunc — the shape every
// downstream middleware expects for its OnFail hook.
func (f *failRenderer) asHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { f.Render(w, r) }
}

// trim helper used in scaffolding.
func _trimSpaces(s string) string { return strings.TrimSpace(s) }
