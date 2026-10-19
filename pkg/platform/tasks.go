package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/robfig/cron/v3"

	gokitasynq "github.com/piresc/gokit/asynq"
)

const defaultTaskConcurrency = 5

// TasksSettings declares the cron schedules a service runs, under
// "service.tasks".
type TasksSettings struct {
	Schedules []ScheduleSettings `yaml:"schedules" mapstructure:"schedules"`
}

// ScheduleSettings enqueues Task on a cron spec ("@every 15s", "*/5 * * * *").
type ScheduleSettings struct {
	Task string `yaml:"task" mapstructure:"task"`
	Cron string `yaml:"cron" mapstructure:"cron"`
}

// Tasks enqueues background tasks and runs the worker and the schedules.
type Tasks struct {
	client      *gokitasynq.Client
	logger      *slog.Logger
	addr        string
	password    string
	concurrency int
	schedules   []ScheduleSettings

	mu       sync.Mutex
	handlers map[string]func(context.Context, []byte) error
	started  bool
	server   *gokitasynq.Server
	cancel   context.CancelFunc // stops the schedule goroutines
	ticks    sync.WaitGroup
}

// schedule is a parsed ScheduleSettings.
type schedule struct {
	task     string
	next     func(now time.Time) time.Time
	interval time.Duration
}

// Handle registers h for taskType. Call it before Run. h returning an error
// fails the task attempt.
func (t *Tasks) Handle(taskType string, h func(ctx context.Context, payload []byte) error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		panic(fmt.Sprintf("platform: Tasks.Handle(%q) after Run", taskType))
	}
	t.handlers[taskType] = h
}

// Enqueue queues a task, processed after delay when delay > 0. payload is
// sent as JSON, or as is when it is a []byte. It returns the task ID.
func (t *Tasks) Enqueue(ctx context.Context, taskType string, payload any, delay time.Duration) (string, error) {
	var data []byte
	switch p := payload.(type) {
	case nil:
	case []byte:
		data = p
	default:
		var err error
		if data, err = json.Marshal(p); err != nil {
			return "", fmt.Errorf("marshal %s payload: %w", taskType, err)
		}
	}
	return t.client.Enqueue(ctx, taskType, data, delay)
}

// start validates the schedules, then starts the worker and one goroutine per
// schedule. Nothing is started when validation fails.
func (t *Tasks) start(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.started = true

	for _, s := range t.schedules {
		if _, ok := t.handlers[s.Task]; !ok {
			return fmt.Errorf("schedule %s has no handler", s.Task)
		}
	}
	parsed := make([]schedule, len(t.schedules))
	for i, s := range t.schedules {
		next, interval, err := parseSchedule(s.Cron)
		if err != nil {
			return fmt.Errorf("schedule %s: %w", s.Task, err)
		}
		parsed[i] = schedule{task: s.Task, next: next, interval: interval}
	}

	srv := gokitasynq.NewServer(t.addr, t.password, t.concurrency)
	for typ, h := range t.handlers {
		srv.Register(typ, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			return h(ctx, task.Payload())
		}))
	}
	if err := srv.Start(); err != nil {
		return err
	}
	t.server = srv

	// Nothing below can fail, so no goroutine outlives a failed start.
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	for _, s := range parsed {
		t.ticks.Add(1)
		go t.runSchedule(ctx, s)
	}
	return nil
}

// runSchedule enqueues s.task at every wall-clock-aligned tick until ctx ends.
func (t *Tasks) runSchedule(ctx context.Context, s schedule) {
	defer t.ticks.Done()
	for {
		fire := s.next(time.Now())
		timer := time.NewTimer(time.Until(fire))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		t.enqueueTick(ctx, s.task, fire, s.interval)
	}
}

// enqueueTick enqueues task for the tick at fire. Every replica computes the
// same fire time, so the task ID is the same everywhere and only the first
// enqueue succeeds. Retention keeps the finished task (and its ID) for one
// interval, so a replica that is late cannot enqueue the tick again. MaxRetry
// is 0: a failed run waits for the next tick.
func (t *Tasks) enqueueTick(ctx context.Context, task string, fire time.Time, interval time.Duration) {
	_, err := t.client.Enqueue(ctx, task, nil, 0,
		asynq.TaskID(fmt.Sprintf("%s:%d", task, fire.Unix())),
		asynq.Retention(interval),
		asynq.MaxRetry(0),
	)
	switch {
	case err == nil:
	case errors.Is(err, asynq.ErrTaskIDConflict):
		t.logger.Debug("scheduled task already enqueued for this tick", "task", task, "tick", fire)
	default:
		t.logger.Error("scheduled task enqueue failed", "task", task, "tick", fire, "error", err)
	}
}

// stopWorker stops the worker, waiting for running handlers. Safe when start
// never ran or failed part way.
func (t *Tasks) stopWorker(context.Context) error {
	t.mu.Lock()
	srv := t.server
	t.server = nil
	t.mu.Unlock()
	if srv != nil {
		srv.Shutdown()
	}
	return nil
}

// stopScheduler stops the schedule goroutines and waits for them. Safe when
// start never ran or failed part way.
func (t *Tasks) stopScheduler(context.Context) error {
	t.mu.Lock()
	cancel := t.cancel
	t.cancel = nil
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	t.ticks.Wait()
	return nil
}

// parseSchedule returns the function that gives the next tick after now, and
// the interval between ticks. Ticks are aligned to the wall clock, not to the
// process start, so every replica fires at the same instants: "@every D" ticks
// at multiples of D, other specs at the cron times.
func parseSchedule(spec string) (func(time.Time) time.Time, time.Duration, error) {
	if rest, ok := strings.CutPrefix(spec, "@every "); ok {
		d, err := time.ParseDuration(strings.TrimSpace(rest))
		if err != nil || d <= 0 {
			return nil, 0, fmt.Errorf("invalid cron %q", spec)
		}
		return func(now time.Time) time.Time { return now.Truncate(d).Add(d) }, d, nil
	}
	sched, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid cron %q: %w", spec, err)
	}
	first := sched.Next(time.Now())
	return sched.Next, sched.Next(first).Sub(first), nil
}

// scheduleInterval is the time between two ticks of spec.
func scheduleInterval(spec string) (time.Duration, error) {
	_, interval, err := parseSchedule(spec)
	return interval, err
}

// initTasks builds Tasks when asynq is enabled and ties the worker and
// schedules to the app lifecycle. Stop funcs run newest-first: the schedules
// stop first, then the worker, then (registered earlier) the event consumers.
func (a *App[S]) initTasks() error {
	if a.AsynqClient == nil {
		return nil
	}
	// asynq connects with RedisClientOpt{Addr, Password}, which cannot select
	// a database, while the rest of the service would use redis.db.
	if a.Config.Redis.DB != 0 {
		return errors.New("asynq requires redis.db 0: asynq cannot select a Redis database")
	}
	concurrency := a.Config.Asynq.Concurrency
	if concurrency <= 0 {
		concurrency = defaultTaskConcurrency
	}
	t := &Tasks{
		client:      a.AsynqClient,
		logger:      a.Logger,
		addr:        fmt.Sprintf("%s:%d", a.Config.Redis.Host, a.Config.Redis.Port),
		password:    a.Config.Redis.Password,
		concurrency: concurrency,
		schedules:   a.Settings.Tasks.Schedules,
		handlers:    make(map[string]func(context.Context, []byte) error),
	}
	a.Tasks = t
	a.onStopNamed("worker", t.stopWorker)
	a.onStopNamed("schedules", t.stopScheduler)
	a.onStart(t.start)
	return nil
}
