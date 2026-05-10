package connections

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestBroker(t *testing.T, max, buf int) *Broker {
	t.Helper()
	return NewBroker(&ConnectionConfig{
		Type:          "sse",
		SubscribePath: "/x",
		MaxClients:    max,
		BufferSize:    buf,
	})
}

func TestBrokerFanOut(t *testing.T) {
	b := newTestBroker(t, 10, 4)

	ch1, c1, ok := b.Subscribe("a")
	if !ok {
		t.Fatal("subscribe a failed")
	}
	defer c1()
	ch2, c2, ok := b.Subscribe("b")
	if !ok {
		t.Fatal("subscribe b failed")
	}
	defer c2()

	b.Publish([]byte("hello"))

	for _, ch := range []<-chan []byte{ch1, ch2} {
		select {
		case msg := <-ch:
			if string(msg) != "hello" {
				t.Errorf("got %q", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for fan-out")
		}
	}
}

func TestBrokerLateJoinerReplaysBuffer(t *testing.T) {
	b := newTestBroker(t, 10, 4)
	b.Publish([]byte("a"))
	b.Publish([]byte("b"))
	b.Publish([]byte("c"))

	ch, cancel, ok := b.Subscribe("late")
	if !ok {
		t.Fatal("subscribe late failed")
	}
	defer cancel()

	got := make([]string, 0, 3)
	timeout := time.After(time.Second)
	for len(got) < 3 {
		select {
		case msg := <-ch:
			got = append(got, string(msg))
		case <-timeout:
			t.Fatalf("only got %v", got)
		}
	}
	want := []string{"a", "b", "c"}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("ring[%d] = %q want %q", i, got[i], v)
		}
	}
}

func TestBrokerSlowSubscriberDoesNotBlock(t *testing.T) {
	b := newTestBroker(t, 10, 1) // tiny per-sub buffer
	_, cancel, ok := b.Subscribe("slow")
	if !ok {
		t.Fatal("subscribe failed")
	}
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish([]byte("x"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publisher blocked by slow subscriber")
	}
	if _, _, dropped := b.Stats(); dropped == 0 {
		t.Error("expected dropped events to be tracked")
	}
}

func TestBrokerMaxClients(t *testing.T) {
	b := newTestBroker(t, 2, 4)
	_, c1, ok1 := b.Subscribe("a")
	_, c2, ok2 := b.Subscribe("b")
	_, _, ok3 := b.Subscribe("c")
	defer c1()
	defer c2()
	if !ok1 || !ok2 || ok3 {
		t.Fatalf("max clients not enforced: ok1=%v ok2=%v ok3=%v", ok1, ok2, ok3)
	}
}

func TestBrokerCancelRemovesSubscriber(t *testing.T) {
	b := newTestBroker(t, 10, 4)
	_, cancel, _ := b.Subscribe("a")
	if b.SubscriberCount() != 1 {
		t.Fatal("expected 1 subscriber")
	}
	cancel()
	if b.SubscriberCount() != 0 {
		t.Errorf("expected 0 after cancel, got %d", b.SubscriberCount())
	}
}

func TestBrokerConcurrentPublishSubscribe(t *testing.T) {
	b := newTestBroker(t, 100, 16)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, cancel, _ := b.Subscribe(randID())
			defer cancel()
			time.Sleep(10 * time.Millisecond)
		}()
	}
	for i := 0; i < 100; i++ {
		b.Publish([]byte("x"))
	}
	wg.Wait()
}

var idSeq atomic.Int64

func randID() string {
	n := idSeq.Add(1)
	return string(rune('A' + n%26))
}
