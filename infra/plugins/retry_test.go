package plugins

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type flakyClient struct {
	failTimes int
	failErr   error
	calls     atomic.Int32
}

func (f *flakyClient) Call(_ context.Context, _ *Request) (*Response, error) {
	n := int(f.calls.Add(1))
	if n <= f.failTimes {
		return nil, f.failErr
	}
	return &Response{Status: 200}, nil
}
func (f *flakyClient) Close() error { return nil }

func TestRetryRecoversAfterFailures(t *testing.T) {
	inner := &flakyClient{failTimes: 2, failErr: errors.New("connection refused")}
	c := wrapWithRetry(inner, &PluginConfig{Retries: 3, RetryBackoff: "1ms"})
	resp, err := c.Call(context.Background(), &Request{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("status = %d", resp.Status)
	}
	if got := inner.calls.Load(); got != 3 {
		t.Errorf("calls = %d", got)
	}
}

func TestRetryGivesUpAfterMax(t *testing.T) {
	inner := &flakyClient{failTimes: 5, failErr: errors.New("connection refused")}
	c := wrapWithRetry(inner, &PluginConfig{Retries: 2, RetryBackoff: "1ms"})
	if _, err := c.Call(context.Background(), &Request{}); err == nil {
		t.Error("expected error after retries exhausted")
	}
	// 1 initial + 2 retries
	if got := inner.calls.Load(); got != 3 {
		t.Errorf("calls = %d", got)
	}
}

func TestRetryDoesNotRetryNonTransient(t *testing.T) {
	inner := &flakyClient{failTimes: 5, failErr: errors.New("permission denied")}
	c := wrapWithRetry(inner, &PluginConfig{Retries: 5, RetryBackoff: "1ms"})
	if _, err := c.Call(context.Background(), &Request{}); err == nil {
		t.Error("expected immediate error")
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("calls = %d (should not retry)", got)
	}
}

func TestRetryStopsOnContextCancel(t *testing.T) {
	inner := &flakyClient{failTimes: 100, failErr: errors.New("connection refused")}
	c := wrapWithRetry(inner, &PluginConfig{Retries: 100, RetryBackoff: "10ms"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, &Request{}); err == nil {
		t.Error("expected context error")
	}
}

type status5xxClient struct{ calls atomic.Int32 }

func (s *status5xxClient) Call(_ context.Context, _ *Request) (*Response, error) {
	s.calls.Add(1)
	return &Response{Status: 503}, nil
}
func (s *status5xxClient) Close() error { return nil }

func TestRetryOn5xx(t *testing.T) {
	inner := &status5xxClient{}
	c := wrapWithRetry(inner, &PluginConfig{Retries: 2, RetryBackoff: "1ms"})
	resp, err := c.Call(context.Background(), &Request{})
	if err == nil {
		// Last attempt also 5xx — surfaced as the response, not an error
		// (we shouldn't lose the body the upstream returned).
		if resp.Status != 503 {
			t.Errorf("final status = %d", resp.Status)
		}
	}
	if got := inner.calls.Load(); got != 3 {
		t.Errorf("5xx calls = %d, want 3", got)
	}
}

func TestRetryDisabledWhenZero(t *testing.T) {
	inner := &flakyClient{failTimes: 5, failErr: errors.New("connection refused")}
	c := wrapWithRetry(inner, &PluginConfig{Retries: 0})
	if _, err := c.Call(context.Background(), &Request{}); err == nil {
		t.Error("expected error")
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("calls = %d", got)
	}
}
