package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	gokitnats "github.com/piresc/gokit/nats"
)

func testSettings() EventsSettings {
	return EventsSettings{
		Streams:   []StreamSettings{{Name: "T", Subjects: []string{"t.>"}}},
		Consumers: []ConsumerSettings{{Name: "c1", Stream: "T", Subject: "t.x"}},
	}
}

// newTestEvents starts an in-process JetStream server and an Events on it.
func newTestEvents(t *testing.T, s EventsSettings) *Events {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = natsserver.RANDOM_PORT
	opts.JetStream = true
	opts.StoreDir = t.TempDir()
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)

	client, err := gokitnats.NewClient(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats client: %v", err)
	}
	t.Cleanup(func() { client.Close(context.Background()) })

	e, err := NewEvents(context.Background(), client, slog.New(slog.NewTextHandler(io.Discard, nil)), s)
	if err != nil {
		t.Fatalf("NewEvents: %v", err)
	}
	return e
}

func startEvents(t *testing.T, e *Events) {
	t.Helper()
	if err := e.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.stop(context.Background()) })
}

func TestPublishSubscribe_RoundTrip(t *testing.T) {
	e := newTestEvents(t, testSettings())
	got := make(chan Event, 1)
	if err := e.Subscribe("c1", func(_ context.Context, ev Event) error { got <- ev; return nil }); err != nil {
		t.Fatal(err)
	}
	startEvents(t, e)

	if err := e.Publish(context.Background(), "t.x", map[string]int{"a": 1}, "id-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-got:
		if string(ev.Data) != `{"a":1}` || ev.ID != "id-1" || ev.Subject != "t.x" {
			t.Fatalf("event = %+v data=%s", ev, ev.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no delivery")
	}
}

func TestPublish_DedupByMsgID(t *testing.T) {
	e := newTestEvents(t, testSettings())
	var n atomic.Int32
	if err := e.Subscribe("c1", func(context.Context, Event) error { n.Add(1); return nil }); err != nil {
		t.Fatal(err)
	}
	startEvents(t, e)

	for range 2 {
		if err := e.Publish(context.Background(), "t.x", map[string]int{"a": 1}, "same"); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(500 * time.Millisecond)
	if n.Load() != 1 {
		t.Fatalf("deliveries = %d, want 1", n.Load())
	}
}

func TestSubscribe_RetryThenAck(t *testing.T) {
	e := newTestEvents(t, testSettings())
	e.nakDelay = 20 * time.Millisecond
	var n atomic.Int32
	done := make(chan struct{})
	if err := e.Subscribe("c1", func(context.Context, Event) error {
		if n.Add(1) == 1 {
			return errors.New("transient")
		}
		close(done)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	startEvents(t, e)

	if err := e.Publish(context.Background(), "t.x", map[string]int{"a": 1}, "r1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler not retried")
	}
	if n.Load() != 2 {
		t.Fatalf("calls = %d, want 2", n.Load())
	}
}

func TestSubscribe_PermanentErrorTerminates(t *testing.T) {
	e := newTestEvents(t, testSettings())
	e.nakDelay = 10 * time.Millisecond
	var n atomic.Int32
	if err := e.Subscribe("c1", func(context.Context, Event) error {
		n.Add(1)
		return fmt.Errorf("bad: %w", ErrPermanent)
	}); err != nil {
		t.Fatal(err)
	}
	startEvents(t, e)

	if err := e.Publish(context.Background(), "t.x", map[string]int{"a": 1}, "p1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if n.Load() != 1 {
		t.Fatalf("calls = %d, want 1", n.Load())
	}
}

func TestSubscribe_UndeclaredConsumer(t *testing.T) {
	e := newTestEvents(t, testSettings())
	err := e.Subscribe("nope", func(context.Context, Event) error { return nil })
	want := "consumer nope not declared in service.events.consumers"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestSubscribe_Twice(t *testing.T) {
	e := newTestEvents(t, testSettings())
	h := func(context.Context, Event) error { return nil }
	if err := e.Subscribe("c1", h); err != nil {
		t.Fatal(err)
	}
	if err := e.Subscribe("c1", h); err == nil {
		t.Fatal("second Subscribe must fail")
	}
}

func TestStop_WithoutStart(t *testing.T) {
	e := newTestEvents(t, testSettings())
	if err := e.stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribe_DrainsOnShutdown(t *testing.T) {
	e := newTestEvents(t, testSettings())
	started := make(chan struct{})
	var finished atomic.Bool
	var once sync.Once
	if err := e.Subscribe("c1", func(context.Context, Event) error {
		once.Do(func() { close(started) })
		time.Sleep(300 * time.Millisecond)
		finished.Store(true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	startEvents(t, e)

	if err := e.Publish(context.Background(), "t.x", map[string]int{"a": 1}, "d1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}
	if err := e.stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !finished.Load() {
		t.Fatal("stop returned before the in-flight handler finished")
	}
}
