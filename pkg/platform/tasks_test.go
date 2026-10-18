package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/piresc/gokit/testkit"
)

// redisContainer starts a Redis container, or skips the test without Docker.
// asynq needs real Redis (Lua, streams); miniredis is not enough.
func redisContainer(t *testing.T) *testkit.RedisContainer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rc, err := testkit.NewRedisContainer(ctx)
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close(context.Background()) })
	return rc
}

func asynqYAML(rc *testkit.RedisContainer) string {
	return fmt.Sprintf("redis:\n  host: %s\n  port: %s\nasynq:\n  enabled: true\n", rc.Host, rc.Port)
}

func newTasksApp(t *testing.T, yaml string) *App[struct{}] {
	t.Helper()
	writeConfig(t, yaml)
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.Tasks == nil {
		t.Fatal("Tasks is nil with asynq enabled")
	}
	return a
}

func TestTasks_EnqueueHandled(t *testing.T) {
	rc := redisContainer(t)
	a := newTasksApp(t, asynqYAML(rc))

	got := make(chan string, 1)
	a.Tasks.Handle("t:echo", func(_ context.Context, payload []byte) error {
		got <- string(payload)
		return nil
	})
	if err := a.Tasks.start(context.Background()); err != nil {
		t.Fatal(err)
	}

	id, err := a.Tasks.Enqueue(context.Background(), "t:echo", map[string]int{"a": 1}, 0)
	if err != nil || id == "" {
		t.Fatalf("Enqueue = %q, %v", id, err)
	}
	select {
	case p := <-got:
		if p != `{"a":1}` {
			t.Fatalf("payload = %s", p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("handler not called")
	}
}

func TestTasks_UniquePerTick(t *testing.T) {
	rc := redisContainer(t)
	a := newTasksApp(t, asynqYAML(rc)+"service:\n  tasks:\n    schedules:\n      - task: t:tick\n        cron: \"@every 1m\"\n")

	// What every replica's scheduler enqueues on a tick.
	opts, err := scheduleOptions("@every 1m")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Tasks.client.Enqueue(context.Background(), "t:tick", nil, 0, opts...); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	_, err = a.Tasks.client.Enqueue(context.Background(), "t:tick", nil, 0, opts...)
	if !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("second enqueue in the same window: want ErrDuplicateTask, got %v", err)
	}
}

func TestScheduleOptions_Interval(t *testing.T) {
	for spec, want := range map[string]time.Duration{
		"@every 15s":  15 * time.Second,
		"@every 1m":   time.Minute,
		"*/5 * * * *": 5 * time.Minute,
	} {
		got, err := scheduleInterval(spec)
		if err != nil || got != want {
			t.Errorf("scheduleInterval(%q) = %v, %v; want %v", spec, got, err, want)
		}
	}
	if _, err := scheduleInterval("not a cron"); err == nil {
		t.Error("want error for invalid cron")
	}
}

func TestTasks_ScheduleWithoutHandler(t *testing.T) {
	mr := newMiniredis(t)
	writeConfig(t, redisYAML(mr)+"asynq:\n  enabled: true\nservice:\n  tasks:\n    schedules:\n      - task: price:refresh\n        cron: \"@every 1m\"\n")
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	err = a.Run()
	if err == nil || err.Error() != "schedule price:refresh has no handler" {
		t.Fatalf("got %v", err)
	}
}

func TestNew_RejectsRedisDBWithAsynq(t *testing.T) {
	mr := newMiniredis(t)
	writeConfig(t, redisYAML(mr)+"  db: 1\nasynq:\n  enabled: true\n")
	_, err := New[struct{}]("svc")
	if err == nil || !strings.Contains(err.Error(), "redis.db 0") {
		t.Fatalf("want redis.db error with asynq enabled, got %v", err)
	}
}

func TestTasks_StopSafeWithoutStart(t *testing.T) {
	mr := newMiniredis(t)
	writeConfig(t, redisYAML(mr)+"asynq:\n  enabled: true\n")
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown without Run: %v", err)
	}
}
