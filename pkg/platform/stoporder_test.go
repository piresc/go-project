package platform

import (
	"context"
	"fmt"
	"strings"
	"testing"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
)

// TestShutdown_StopOrder checks the real hooks stop in order: schedules, task
// worker, event consumers, then the infrastructure closes.
func TestShutdown_StopOrder(t *testing.T) {
	rc := redisContainer(t)

	opts := natstest.DefaultTestOptions
	opts.Port = natsserver.RANDOM_PORT
	opts.JetStream = true
	opts.StoreDir = t.TempDir()
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)

	writeConfig(t, asynqYAML(rc)+fmt.Sprintf("nats:\n  enabled: true\n  url: %s\n", srv.ClientURL()))
	var order []string
	a, err := New[struct{}]("svc", func(o *options) {
		o.stopTrace = func(name string) { order = append(order, name) }
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Events == nil || a.Tasks == nil {
		t.Fatalf("Events=%v Tasks=%v, want both set", a.Events, a.Tasks)
	}

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(order, ","), "schedules,worker,events,hooks-done"; got != want {
		t.Fatalf("stop order = %s, want %s", got, want)
	}
	// hooks-done is the last hook; infrastructure closes after it.
	if err := a.Redis.GetClient().Ping(context.Background()).Err(); err == nil {
		t.Fatal("redis still open after Shutdown")
	}
}
