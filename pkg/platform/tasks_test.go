package platform

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
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
	yaml := asynqYAML(rc)
	a1 := newTasksApp(t, yaml)
	a2 := newTasksApp(t, yaml) // a second replica on the same Redis

	var runs atomic.Int64
	a1.Tasks.Handle("t:tick", func(context.Context, []byte) error { runs.Add(1); return nil })
	if err := a1.Tasks.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: rc.ConnectionString()})
	t.Cleanup(func() { _ = inspector.Close() })

	ctx := context.Background()
	tick := time.Now().Truncate(time.Minute)
	a1.Tasks.enqueueTick(ctx, "t:tick", tick, time.Minute)

	// Wait until the task is completed, which is when asynq would release a
	// Unique lock. The second replica's enqueue for the same tick must still
	// be rejected.
	id := fmt.Sprintf("t:tick:%d", tick.Unix())
	waitFor(t, func() bool {
		info, err := inspector.GetTaskInfo("default", id)
		return err == nil && info.State == asynq.TaskStateCompleted
	})
	a2.Tasks.enqueueTick(ctx, "t:tick", tick, time.Minute)
	time.Sleep(time.Second)
	if got := runs.Load(); got != 1 {
		t.Fatalf("handler ran %d times for one tick, want 1", got)
	}

	a2.Tasks.enqueueTick(ctx, "t:tick", tick.Add(time.Minute), time.Minute)
	waitFor(t, func() bool { return runs.Load() == 2 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 10s")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestParseSchedule_Aligned(t *testing.T) {
	next, interval, err := parseSchedule("@every 15s")
	if err != nil || interval != 15*time.Second {
		t.Fatalf("parseSchedule = %v, %v", interval, err)
	}
	base := time.Unix(1_700_000_010, 0) // start of the window [..010, ..025)
	a := next(base.Add(1 * time.Second))
	b := next(base.Add(8 * time.Second))
	if !a.Equal(b) {
		t.Fatalf("same window gave different ticks: %v vs %v", a, b)
	}
	if a.Unix()%15 != 0 {
		t.Fatalf("tick %d is not a multiple of 15s", a.Unix())
	}
	if !next(a).Equal(a.Add(15 * time.Second)) {
		t.Fatalf("tick after %v = %v", a, next(a))
	}

	cronNext, _, err := parseSchedule("*/5 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	c := cronNext(time.Unix(1_700_000_010, 0))
	if c.Unix()%300 != 0 {
		t.Fatalf("cron tick %d not aligned", c.Unix())
	}
}

func TestTasks_StopBeforeStartAndAfterFailedStart(t *testing.T) {
	tk := &Tasks{handlers: map[string]func(context.Context, []byte) error{}}
	if err := tk.stopScheduler(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A schedule with an invalid cron fails start before any goroutine runs.
	tk = &Tasks{
		handlers:  map[string]func(context.Context, []byte) error{"x": nil},
		schedules: []ScheduleSettings{{Task: "x", Cron: "bogus"}},
	}
	if err := tk.start(context.Background()); err == nil {
		t.Fatal("want error for invalid cron")
	}
	if err := tk.stopScheduler(context.Background()); err != nil {
		t.Fatal(err)
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
