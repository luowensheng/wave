package httpclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Execute performs the HTTP request with vars substituted into URL and Body.
// Variable substitution uses {{varname}} syntax.
//
// Returns a result map with self-documenting, type-explicit fields:
//
//   text         string             — raw response body as a string (always present)
//   json         map[string]any     — parsed body, present iff the body parses as a JSON object
//   xml          map[string]any     — parsed body, present iff the body parses as well-formed XML
//   status       int                — HTTP status code
//   status_text  string             — full HTTP status line (e.g. "200 OK")
//   headers      map[string]string  — response headers (first value per key)
//
// Field names are explicit so YAML readers can tell the type at a glance.
// There is NO implicit top-level merge of parsed JSON/XML keys — to read a
// JSON field, write the explicit "json.<field>" path; for XML, "xml.<field>".
//
// XML→map conversion rules (explicit and deterministic — the rules are the
// contract, there is no magic):
//   - Decoding uses the stdlib streaming xml.Decoder. No third-party deps.
//   - An element's key is its LOCAL name: any namespace prefix is stripped
//     (content:encoded → encoded, atom:link → link, dc:creator → creator).
//   - A leaf element with only text → the trimmed text string.
//   - An element with child elements and/or attributes → a map[string]any.
//   - Attributes become keys prefixed with "@" (<link href="x"/> → {"@href":"x"}).
//   - Mixed text + children/attrs → the trimmed text goes under key "#text".
//   - Repeated sibling elements sharing a name → a []any in document order;
//     a name occurring exactly once is NOT wrapped in a slice. (This is the
//     standard generic XML→map convention and the known single-vs-many edge.)
//   - The root element is the top-level key: an RSS doc is
//     result["xml"]["rss"]["channel"]["item"], Atom is
//     result["xml"]["feed"]["entry"].
//   - A malformed / non-XML body leaves "xml" absent (same as "json").
func (def *RequestDef) Execute(ctx context.Context, vars map[string]any) (map[string]any, error) {
	method := strings.ToUpper(def.Method)
	if method == "" {
		method = "GET"
	}

	// Substitute vars into URL and Body.
	url := substituteVars(os.ExpandEnv(def.URL), vars)
	body := substituteVars(def.Body, vars)

	timeout := time.Duration(def.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: def.InsecureSkipVerify}, //nolint:gosec
	}

	followRedirects := true
	if def.FollowRedirects != nil {
		followRedirects = *def.FollowRedirects
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	if !followRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	retryCount := def.RetryCount
	retryDelay := time.Duration(def.RetryDelay) * time.Second
	if retryDelay == 0 {
		retryDelay = time.Second
	}

	var lastErr error
	for attempt := 0; attempt <= retryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay):
			}
		}

		var bodyReader io.Reader
		if body != "" {
			bodyReader = strings.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}

		for k, v := range def.Headers {
			req.Header.Set(k, os.ExpandEnv(v))
		}

		if def.Auth != nil {
			req.SetBasicAuth(def.Auth.Username, def.Auth.Password)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue // retry on network error
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		// Collapse multi-value headers to first value — keeps the dot-path
		// shape simple. If you need every value, switch to a plugin.
		headers := make(map[string]string, len(resp.Header))
		for k, vv := range resp.Header {
			if len(vv) > 0 {
				headers[k] = vv[0]
			}
		}

		result := map[string]any{
			"text":        string(respBody),
			"status":      resp.StatusCode,
			"status_text": resp.Status,
			"headers":     headers,
		}
		// Only set "json" when the body decodes cleanly as a JSON object.
		// JSON arrays / scalars / non-JSON bodies leave it absent so callers
		// can detect the case with `if hasvalue "json"`.
		var parsed map[string]any
		if err := json.Unmarshal(respBody, &parsed); err == nil {
			result["json"] = parsed
		}
		// Only set "xml" when the body parses as well-formed XML. Non-XML
		// and malformed bodies leave it absent so callers can detect the
		// case with `if hasvalue "xml"`. Conversion rules are documented on
		// the Execute doc comment above.
		if x, ok := parseXML(respBody); ok {
			result["xml"] = x
		}
		return result, nil
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", retryCount+1, lastErr)
}

// substituteVars replaces {{key}} with the string value of vars[key].
// A []byte var (e.g. a body_raw input forwarded as a loopback body)
// substitutes as its UTF-8 string, not Go's "%v" byte-number form.
func substituteVars(s string, vars map[string]any) string {
	for k, v := range vars {
		var sv string
		switch t := v.(type) {
		case []byte:
			sv = string(t)
		case string:
			sv = t
		default:
			sv = fmt.Sprintf("%v", v)
		}
		s = strings.ReplaceAll(s, "{{"+k+"}}", sv)
	}
	return s
}

// parseXML decodes a body as XML and returns it as a map keyed by the root
// element's local name. The bool is false for empty or malformed input — the
// caller leaves the "xml" field absent in that case. Conversion rules are
// documented on the Execute doc comment.
func parseXML(body []byte) (map[string]any, bool) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, false
	}
	dec := xml.NewDecoder(strings.NewReader(trimmed))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue // skip XML declaration, comments, leading whitespace
		}
		val, err := xmlNodeToMap(dec, start)
		if err != nil {
			return nil, false
		}
		return map[string]any{start.Name.Local: val}, true
	}
}

// xmlNodeToMap consumes one element (whose start tag is `start`) from dec and
// returns its converted value: a trimmed string for a pure-text leaf, or a
// map[string]any when the element has attributes and/or child elements.
// Repeated children sharing a local name collapse into a []any in document
// order; a name seen once is stored unwrapped.
func xmlNodeToMap(dec *xml.Decoder, start xml.StartElement) (any, error) {
	node := map[string]any{}
	for _, attr := range start.Attr {
		node["@"+attr.Name.Local] = attr.Value
	}

	var text strings.Builder
	hasChildren := false

	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			hasChildren = true
			child, err := xmlNodeToMap(dec, t)
			if err != nil {
				return nil, err
			}
			addChild(node, t.Name.Local, child)
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			trimmed := strings.TrimSpace(text.String())
			// Pure-text leaf with no attributes → bare string value.
			if !hasChildren && len(node) == 0 {
				return trimmed, nil
			}
			// Mixed content: text alongside children/attrs goes under #text.
			if trimmed != "" {
				node["#text"] = trimmed
			}
			return node, nil
		}
	}
}

// addChild inserts child under key, promoting to a []any (preserving document
// order) when a sibling with the same local name was already seen.
func addChild(node map[string]any, key string, child any) {
	existing, ok := node[key]
	if !ok {
		node[key] = child
		return
	}
	if slice, ok := existing.([]any); ok {
		node[key] = append(slice, child)
		return
	}
	node[key] = []any{existing, child}
}
