package redirect

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedirect_DefaultStatusIs302(t *testing.T) {
	h, err := (&Config{RedirectURL: "https://example.com/"}).CreateRoute("GET", "/old", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest("GET", "/old", nil))
	if rr.Code != http.StatusFound {
		t.Fatalf("got %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "https://example.com/" {
		t.Fatalf("Location=%q", loc)
	}
}

func TestRedirect_CustomStatus(t *testing.T) {
	cases := []int{301, 302, 303, 307, 308}
	for _, sc := range cases {
		h, err := (&Config{RedirectURL: "https://example.com/", StatusCode: sc}).CreateRoute("GET", "/x", nil)
		if err != nil {
			t.Fatalf("status=%d: %v", sc, err)
		}
		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest("GET", "/x", nil))
		if rr.Code != sc {
			t.Fatalf("status=%d: got %d", sc, rr.Code)
		}
	}
}

func TestRedirect_QueryStringForwarded(t *testing.T) {
	h, err := (&Config{RedirectURL: "https://example.com/landing"}).CreateRoute("GET", "/old", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest("GET", "/old?utm=email&campaign=spring", nil))
	loc := rr.Header().Get("Location")
	if loc != "https://example.com/landing?utm=email&campaign=spring" {
		t.Fatalf("got Location=%q", loc)
	}
}

func TestRedirect_RejectsMissingURL(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t\n"} {
		if _, err := (&Config{RedirectURL: raw}).CreateRoute("GET", "/", nil); err == nil {
			t.Fatalf("expected error for missing URL %q", raw)
		}
	}
}

func TestRedirect_RejectsRelativeURL(t *testing.T) {
	for _, raw := range []string{"/relative", "./local", "foo.html"} {
		if _, err := (&Config{RedirectURL: raw}).CreateRoute("GET", "/", nil); err == nil {
			t.Fatalf("expected error for relative URL %q", raw)
		}
	}
}

func TestRedirect_RejectsInvalidStatus(t *testing.T) {
	for _, sc := range []int{200, 201, 299, 400, 404, 500} {
		if _, err := (&Config{RedirectURL: "https://example.com/", StatusCode: sc}).CreateRoute("GET", "/", nil); err == nil {
			t.Fatalf("expected error for status %d", sc)
		}
	}
}

func TestRedirect_AcceptsSchemes(t *testing.T) {
	for _, u := range []string{"https://example.com/", "http://example.com/", "ftp://files.example.com/"} {
		if _, err := (&Config{RedirectURL: u}).CreateRoute("GET", "/", nil); err != nil {
			t.Fatalf("scheme test %q: %v", u, err)
		}
	}
}
