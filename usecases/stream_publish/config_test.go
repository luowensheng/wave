package stream_publish

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wave/infra/connections"
)

func TestStreamPublishFiltersFieldsAndFansOut(t *testing.T) {
	// Set up a broker registry with one connection.
	reg, err := connections.NewRegistry(map[string]*connections.ConnectionConfig{
		"payments": {
			Type:          "sse",
			SubscribePath: "/events/payments",
			BufferSize:    16,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	connections.SetDefault(reg)

	broker, _ := reg.Get("payments")
	ch, cancel, _ := broker.Subscribe("test")
	defer cancel()

	// Build a stream-publish handler.
	cfg := &Config{
		Connection: "payments",
		EventType:  "payment",
		Output: map[string]string{
			"payment_id": "response.id",
			"amount":     "response.amount",
		},
		StaticMeta: map[string]string{"source": "stripe"},
	}
	h, err := cfg.CreateRoute("POST", "/webhooks/stripe", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Drain the initial buffer (none yet) and POST a webhook payload.
	body := bytes.NewReader([]byte(`{"id":"pi_123","amount":2000,"secret":"sk_x"}`))
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", body)
	h(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rr.Code)
	}

	select {
	case msg := <-ch:
		s := string(msg)
		if !strings.HasPrefix(s, "event: payment\n") {
			t.Errorf("missing event line: %q", s)
		}
		if !strings.Contains(s, `"payment_id":"pi_123"`) {
			t.Errorf("missing payment_id: %q", s)
		}
		if !strings.Contains(s, `"amount":2000`) {
			t.Errorf("missing amount: %q", s)
		}
		if !strings.Contains(s, `"source":"stripe"`) {
			t.Errorf("missing static_meta: %q", s)
		}
		if strings.Contains(s, `"secret"`) {
			t.Errorf("secret leaked: %q", s)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for fan-out")
	}
}

func TestStreamPublishMissingConnectionErrors(t *testing.T) {
	reg, _ := connections.NewRegistry(map[string]*connections.ConnectionConfig{
		"x": {Type: "sse", SubscribePath: "/x"},
	})
	connections.SetDefault(reg)

	cfg := &Config{Connection: "doesnotexist"}
	h, err := cfg.CreateRoute("POST", "/y", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/y", strings.NewReader("{}"))
	h(rr, r)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}
