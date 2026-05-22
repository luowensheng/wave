package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func boolPtr(b bool) *bool { return &b }

func TestExecute(t *testing.T) {
	t.Run("JSON body decoded and merged", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"temperature":18.5,"city":"London"}`))
		}))
		defer ts.Close()

		def := &RequestDef{URL: ts.URL}
		result, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result["status"] != 200 {
			t.Errorf("expected status 200, got %v", result["status"])
		}
		if result["text"] != `{"temperature":18.5,"city":"London"}` {
			t.Errorf("unexpected text: %v", result["text"])
		}
		// JSON-parsed body must live under "json", NOT merged at top level.
		j, ok := result["json"].(map[string]any)
		if !ok {
			t.Fatalf("expected result[json] to be map, got %T", result["json"])
		}
		if j["temperature"] != 18.5 {
			t.Errorf("expected json.temperature 18.5, got %v", j["temperature"])
		}
		if j["city"] != "London" {
			t.Errorf("expected json.city London, got %v", j["city"])
		}
		// Top-level merge MUST NOT happen — that was the implicit behavior we removed.
		for _, key := range []string{"temperature", "city"} {
			if _, ok := result[key]; ok {
				t.Errorf("unexpected top-level key %q — JSON should only be under .json", key)
			}
		}
	})

	t.Run("Non-JSON body — text only, no json key", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("hello"))
		}))
		defer ts.Close()

		def := &RequestDef{URL: ts.URL}
		result, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result["text"] != "hello" {
			t.Errorf("expected text 'hello', got %v", result["text"])
		}
		if result["status"] != 200 {
			t.Errorf("expected status 200, got %v", result["status"])
		}
		if _, ok := result["json"]; ok {
			t.Error("non-JSON body should not produce a json key")
		}
	})

	t.Run("Headers exposed under headers map", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Trace-Id", "abc-123")
			w.Write([]byte(`{}`))
		}))
		defer ts.Close()

		def := &RequestDef{URL: ts.URL}
		result, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		hdrs, ok := result["headers"].(map[string]string)
		if !ok {
			t.Fatalf("expected headers to be map[string]string, got %T", result["headers"])
		}
		if hdrs["X-Trace-Id"] != "abc-123" {
			t.Errorf("expected X-Trace-Id=abc-123, got %q", hdrs["X-Trace-Id"])
		}
	})

	t.Run("varname substitution in URL", func(t *testing.T) {
		var receivedQuery string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedQuery = r.URL.RawQuery
			w.Write([]byte(`{}`))
		}))
		defer ts.Close()

		def := &RequestDef{URL: ts.URL + "/weather?q={{city}}"}
		vars := map[string]any{"city": "Paris"}
		_, err := def.Execute(context.Background(), vars)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedQuery != "q=Paris" {
			t.Errorf("expected query 'q=Paris', got %q", receivedQuery)
		}
	})

	t.Run("ENV_VAR expansion in URL", func(t *testing.T) {
		t.Setenv("TEST_KEY", "secret")

		var receivedQuery string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedQuery = r.URL.Query().Get("token")
			w.Write([]byte(`{}`))
		}))
		defer ts.Close()

		def := &RequestDef{URL: ts.URL + "?token=$TEST_KEY"}
		_, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedQuery != "secret" {
			t.Errorf("expected token 'secret', got %q", receivedQuery)
		}
	})

	t.Run("ENV_VAR expansion in headers", func(t *testing.T) {
		t.Setenv("AUTH_TOKEN", "mytoken123")

		var receivedAuth string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAuth = r.Header.Get("X-Api-Key")
			w.Write([]byte(`{}`))
		}))
		defer ts.Close()

		def := &RequestDef{
			URL:     ts.URL,
			Headers: map[string]string{"X-Api-Key": "$AUTH_TOKEN"},
		}
		_, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedAuth != "mytoken123" {
			t.Errorf("expected header 'mytoken123', got %q", receivedAuth)
		}
	})

	t.Run("Retry on network error", func(t *testing.T) {
		var attempts int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&attempts, 1)
			if n < 3 {
				// close connection immediately to simulate network error
				hj, ok := w.(http.Hijacker)
				if ok {
					conn, _, _ := hj.Hijack()
					conn.Close()
					return
				}
			}
			w.Write([]byte(`{"ok":true}`))
		}))
		defer ts.Close()

		def := &RequestDef{
			URL:        ts.URL,
			RetryCount: 2,
			RetryDelay: 0, // will default to 1s — override via zero which becomes 1s
		}
		// Use a short retry delay by setting RetryDelay=0 (defaults to 1s).
		// To speed up the test, we set it to 0 but that still defaults to 1s in Execute.
		// Accept the test taking up to ~2s for two retries.
		result, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error after retries: %v", err)
		}

		total := atomic.LoadInt32(&attempts)
		if total != 3 {
			t.Errorf("expected 3 total attempts, got %d", total)
		}
		j, ok := result["json"].(map[string]any)
		if !ok {
			t.Fatalf("expected result.json to be map, got %T", result["json"])
		}
		if j["ok"] != true {
			t.Errorf("expected json.ok=true, got %v", j["ok"])
		}
	})

	t.Run("Timeout respected - no error within generous timeout", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond)
			w.Write([]byte(`{"done":true}`))
		}))
		defer ts.Close()

		def := &RequestDef{URL: ts.URL, Timeout: 5}
		_, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error with generous timeout: %v", err)
		}
	})

	t.Run("Timeout triggers error when server is slow", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			w.Write([]byte(`{}`))
		}))
		defer ts.Close()

		def := &RequestDef{URL: ts.URL, Timeout: 1}
		_, err := def.Execute(context.Background(), nil)
		if err == nil {
			t.Error("expected timeout error, got nil")
		}
	})

	t.Run("Basic auth sent", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "user" || pass != "pass" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"authed":true}`))
		}))
		defer ts.Close()

		def := &RequestDef{
			URL:  ts.URL,
			Auth: &RequestAuth{Username: "user", Password: "pass"},
		}
		result, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result["status"] != 200 {
			t.Errorf("expected 200, got %v", result["status"])
		}
	})

	t.Run("FollowRedirects false stops at 302", func(t *testing.T) {
		dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"final":true}`))
		}))
		defer dest.Close()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, dest.URL, http.StatusFound)
		}))
		defer ts.Close()

		follow := false
		def := &RequestDef{URL: ts.URL, FollowRedirects: &follow}
		result, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result["status"] != 302 {
			t.Errorf("expected status 302, got %v", result["status"])
		}
	})

	t.Run("Method defaults to GET", func(t *testing.T) {
		var receivedMethod string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			w.Write([]byte(`{}`))
		}))
		defer ts.Close()

		def := &RequestDef{URL: ts.URL} // Method is empty
		_, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedMethod != "GET" {
			t.Errorf("expected method GET, got %q", receivedMethod)
		}
	})
}

// xmlServer spins up an httptest server returning the given XML body.
func xmlServer(t *testing.T, body string) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestExecuteXML(t *testing.T) {
	t.Run("XML object body — nested path, leaf text is string", func(t *testing.T) {
		url := xmlServer(t, `<root><meta><name>hello</name><count>3</count></meta></root>`)
		def := &RequestDef{URL: url}
		result, err := def.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		x, ok := result["xml"].(map[string]any)
		if !ok {
			t.Fatalf("expected result[xml] map, got %T", result["xml"])
		}
		root := x["root"].(map[string]any)
		meta := root["meta"].(map[string]any)
		if name, ok := meta["name"].(string); !ok || name != "hello" {
			t.Errorf("expected meta.name string 'hello', got %#v", meta["name"])
		}
		if meta["count"] != "3" {
			t.Errorf("expected leaf text string '3', got %#v", meta["count"])
		}
		// text still always present.
		if result["text"] == "" {
			t.Error("text should still be set")
		}
	})

	t.Run("Repeated elements → slice in document order", func(t *testing.T) {
		url := xmlServer(t, `<list><item>a</item><item>b</item><item>c</item></list>`)
		def := &RequestDef{URL: url}
		result, _ := def.Execute(context.Background(), nil)
		list := result["xml"].(map[string]any)["list"].(map[string]any)
		items, ok := list["item"].([]any)
		if !ok {
			t.Fatalf("expected item slice, got %T", list["item"])
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		if items[0] != "a" || items[1] != "b" || items[2] != "c" {
			t.Errorf("document order wrong: %#v", items)
		}
	})

	t.Run("Single element NOT wrapped in slice", func(t *testing.T) {
		url := xmlServer(t, `<list><item>only</item></list>`)
		def := &RequestDef{URL: url}
		result, _ := def.Execute(context.Background(), nil)
		list := result["xml"].(map[string]any)["list"].(map[string]any)
		if s, ok := list["item"].([]any); ok {
			t.Errorf("single element must not be a slice, got %#v", s)
		}
		if list["item"] != "only" {
			t.Errorf("expected 'only', got %#v", list["item"])
		}
	})

	t.Run("Attributes → @-prefixed key, reachable via xml.<...>.link.@href", func(t *testing.T) {
		url := xmlServer(t, `<feed><link href="https://example.com/a" rel="alternate"/></feed>`)
		def := &RequestDef{URL: url}
		result, _ := def.Execute(context.Background(), nil)
		feed := result["xml"].(map[string]any)["feed"].(map[string]any)
		link := feed["link"].(map[string]any)
		if link["@href"] != "https://example.com/a" {
			t.Errorf("expected @href, got %#v", link["@href"])
		}
		if link["@rel"] != "alternate" {
			t.Errorf("expected @rel, got %#v", link["@rel"])
		}
	})

	t.Run("Namespaced element → local-name key", func(t *testing.T) {
		url := xmlServer(t, `<rss xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:dc="http://purl.org/dc/elements/1.1/"><item><content:encoded>HTML</content:encoded><atom:link href="x"/><dc:creator>Jane</dc:creator></item></rss>`)
		def := &RequestDef{URL: url}
		result, _ := def.Execute(context.Background(), nil)
		item := result["xml"].(map[string]any)["rss"].(map[string]any)["item"].(map[string]any)
		if item["encoded"] != "HTML" {
			t.Errorf("expected local-name 'encoded'=HTML, got %#v", item["encoded"])
		}
		if l, ok := item["link"].(map[string]any); !ok || l["@href"] != "x" {
			t.Errorf("expected local-name 'link' with @href, got %#v", item["link"])
		}
		if item["creator"] != "Jane" {
			t.Errorf("expected local-name 'creator'=Jane, got %#v", item["creator"])
		}
	})

	t.Run("Mixed content → #text key", func(t *testing.T) {
		url := xmlServer(t, `<p>hello <b>bold</b></p>`)
		def := &RequestDef{URL: url}
		result, _ := def.Execute(context.Background(), nil)
		p := result["xml"].(map[string]any)["p"].(map[string]any)
		if p["#text"] != "hello" {
			t.Errorf("expected #text 'hello', got %#v", p["#text"])
		}
		if p["b"] != "bold" {
			t.Errorf("expected b 'bold', got %#v", p["b"])
		}
	})

	t.Run("Non-XML body — no xml key, text still set", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("just plain text"))
		}))
		defer ts.Close()
		def := &RequestDef{URL: ts.URL}
		result, _ := def.Execute(context.Background(), nil)
		if _, ok := result["xml"]; ok {
			t.Error("non-XML body must not produce xml key")
		}
		if result["text"] != "just plain text" {
			t.Errorf("text still expected, got %#v", result["text"])
		}
	})

	t.Run("Malformed XML — no xml key", func(t *testing.T) {
		url := xmlServer(t, `<root><unclosed></root>`)
		def := &RequestDef{URL: url}
		result, _ := def.Execute(context.Background(), nil)
		if _, ok := result["xml"]; ok {
			t.Error("malformed XML must not produce xml key")
		}
	})

	t.Run("Realistic RSS 2.0 → xml.rss.channel.item slice", func(t *testing.T) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <title>Example Feed</title>
    <link>https://example.com</link>
    <item>
      <title>First Post</title>
      <link>https://example.com/1</link>
      <description>Desc one</description>
      <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
      <category>tech</category>
    </item>
    <item>
      <title>Second Post</title>
      <link>https://example.com/2</link>
      <description>Desc two</description>
      <pubDate>Tue, 02 Jan 2024 00:00:00 GMT</pubDate>
    </item>
    <item>
      <title>Third Post</title>
      <link>https://example.com/3</link>
      <description>Desc three</description>
      <pubDate>Wed, 03 Jan 2024 00:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`
		url := xmlServer(t, rss)
		def := &RequestDef{URL: url}
		result, _ := def.Execute(context.Background(), nil)
		channel := result["xml"].(map[string]any)["rss"].(map[string]any)["channel"].(map[string]any)
		items, ok := channel["item"].([]any)
		if !ok {
			t.Fatalf("expected channel.item slice, got %T", channel["item"])
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		first := items[0].(map[string]any)
		if first["title"] != "First Post" || first["link"] != "https://example.com/1" {
			t.Errorf("first item wrong: %#v", first)
		}
		third := items[2].(map[string]any)
		if third["title"] != "Third Post" {
			t.Errorf("expected document order, got %#v", third["title"])
		}
	})

	t.Run("Realistic Atom → xml.feed.entry slice", func(t *testing.T) {
		atom := `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Atom Example</title>
  <entry>
    <title>Atom One</title>
    <link href="https://example.com/a1" rel="alternate"/>
    <updated>2024-01-01T00:00:00Z</updated>
  </entry>
  <entry>
    <title>Atom Two</title>
    <link href="https://example.com/a2" rel="alternate"/>
    <updated>2024-01-02T00:00:00Z</updated>
  </entry>
</feed>`
		url := xmlServer(t, atom)
		def := &RequestDef{URL: url}
		result, _ := def.Execute(context.Background(), nil)
		feed := result["xml"].(map[string]any)["feed"].(map[string]any)
		entries, ok := feed["entry"].([]any)
		if !ok {
			t.Fatalf("expected feed.entry slice, got %T", feed["entry"])
		}
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
		e0 := entries[0].(map[string]any)
		if e0["title"] != "Atom One" {
			t.Errorf("expected 'Atom One', got %#v", e0["title"])
		}
		link := e0["link"].(map[string]any)
		if link["@href"] != "https://example.com/a1" {
			t.Errorf("expected entry link @href, got %#v", link["@href"])
		}
	})
}
