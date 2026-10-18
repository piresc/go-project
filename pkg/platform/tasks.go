package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// Tasks enqueues background tasks and runs the worker and scheduler.
type Tasks struct {
	client      *gokitasynq.Client
	logger      *slog.Logger
	addr        string
	password    string
	concurrency int
	schedules   []ScheduleSettings

	mu        sync.Mutex
	handlers  map[string]func(context.Context, []byte) error
	started   bool
	server    *gokitasynq.Server
	scheduler *asynq.Scheduler
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

// start validates the schedules, then starts the worker and the scheduler.
func (t *Tasks) start(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.started = true

	for _, s := range t.schedules {
		if _, ok := t.handlers[s.Task]; !ok {
			return fmt.Errorf("schedule %s has no handler", s.Task)
		}
	}
	// Parse every cron spec before starting anything.
	opts := make([][]asynq.Option, len(t.schedules))
	for i, s := range t.schedules {
		o, err := scheduleOptions(s.Cron)
		if err != nil {
			return fmt.Errorf("schedule %s: %w", s.Task, err)
		}
		opts[i] = o
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

	if len(t.schedules) == 0 {
		return nil
	}
	sch := asynq.NewScheduler(asynq.RedisClientOpt{Addr: t.addr, Password: t.password}, &asynq.SchedulerOpts{
		Logger:              slogAsynq{t.logger},
		EnqueueErrorHandler: t.scheduleEnqueueFailed,
	})
	for i, s := range t.schedules {
		if _, err := sch.Register(s.Cron, asynq.NewTask(s.Task, nil), opts[i]...); err != nil {
			return fmt.Errorf("schedule %s: %w", s.Task, err)
		}
	}
	if err := sch.Start(); err != nil {
		return fmt.Errorf("start scheduler: %w", err)
	}
	t.scheduler = sch
	return nil
}

func (t *Tasks) scheduleEnqueueFailed(task *asynq.Task, _ []asynq.Option, err error) {
	if errors.Is(err, asynq.ErrDuplicateTask) {
		t.logger.Debug("scheduled task already enqueued this tick", "task", task.Type())
		return
	}
	t.logger.Error("scheduled task enqueue failed", "task", task.Type(), "error", err)
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

// stopScheduler stops the scheduler. Safe when start never ran or failed part
// way.
func (t *Tasks) stopScheduler(context.Context) error {
	t.mu.Lock()
	sch := t.scheduler
	t.scheduler = nil
	t.mu.Unlock()
	if sch != nil {
		sch.Shutdown()
	}
	return nil
}

// scheduleOptions are the enqueue options for a scheduled task. The task is
// unique for one schedule interval, so N replicas enqueue one task per tick,
// and it never retries: a failed run waits for the next tick.
func scheduleOptions(spec string) ([]asynq.Option, error) {
	interval, err := scheduleInterval(spec)
	if err != nil {
		return nil, err
	}
	return []asynq.Option{asynq.Unique(interval), asynq.MaxRetry(0)}, nil
}

// scheduleInterval is the time between two ticks of spec.
func scheduleInterval(spec string) (time.Duration, error) {
	sched, err := cron.ParseStandard(spec)
	if err != nil {
		return 0, fmt.Errorf("invalid cron %q: %w", spec, err)
	}
	first := sched.Next(time.Now())
	return sched.Next(first).Sub(first), nil
}

// initTasks builds Tasks when asynq is enabled and ties the worker and
// scheduler to the app lifecycle. Stop funcs run newest-first: the scheduler
// stops first, then the worker, then (registered earlier) the event consumers.
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
	a.onStop(t.stopWorker)
	a.onStop(t.stopScheduler)
	a.onStart(t.start)
	return nil
}

// slogAsynq adapts the bootstrap logger to asynq's Logger.
type slogAsynq struct{ l *slog.Logger }

func (s slogAsynq) Debug(args ...any) { s.l.Debug(fmt.Sprint(args...)) }
func (s slogAsynq) Info(args ...any)  { s.l.Info(fmt.Sprint(args...)) }
func (s slogAsynq) Warn(args ...any)  { s.l.Warn(fmt.Sprint(args...)) }
func (s slogAsynq) Error(args ...any) { s.l.Error(fmt.Sprint(args...)) }

// Fatal logs at error level and does not exit, unlike asynq's default.
func (s slogAsynq) Fatal(args ...any) { s.l.Error(fmt.Sprint(args...)) }
