package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	gokitnats "github.com/piresc/gokit/nats"
)

const (
	defaultStreamMaxAge   = 168 * time.Hour
	defaultMaxDeliver     = 5
	defaultAckWait        = 30 * time.Second
	defaultNakDelay       = 5 * time.Second
	msgIDHeader           = "Nats-Msg-Id"
	eventsDeclarationHint = "service.events.consumers"
)

// ErrPermanent marks a handler failure that retrying cannot fix. The message
// is terminated instead of redelivered.
var ErrPermanent = errors.New("permanent event failure")

// EventsSettings declares the JetStream streams and durable consumers a
// service uses, under "service.events". A service that publishes to or
// consumes from a stream declares that stream identically.
type EventsSettings struct {
	Streams   []StreamSettings   `yaml:"streams" mapstructure:"streams"`
	Consumers []ConsumerSettings `yaml:"consumers" mapstructure:"consumers"`
}

// StreamSettings declares a file-backed stream with limits retention.
type StreamSettings struct {
	Name     string   `yaml:"name" mapstructure:"name"`
	Subjects []string `yaml:"subjects" mapstructure:"subjects"`
	// MaxAge defaults to 168h.
	MaxAge time.Duration `yaml:"max_age" mapstructure:"max_age"`
}

// ConsumerSettings declares a durable consumer with explicit ack.
type ConsumerSettings struct {
	Name    string `yaml:"name" mapstructure:"name"`
	Stream  string `yaml:"stream" mapstructure:"stream"`
	Subject string `yaml:"subject" mapstructure:"subject"`
	// MaxDeliver defaults to 5.
	MaxDeliver int `yaml:"max_deliver" mapstructure:"max_deliver"`
	// AckWait defaults to 30s.
	AckWait time.Duration `yaml:"ack_wait" mapstructure:"ack_wait"`
}

// Event is a delivered message. ID is the Nats-Msg-Id header.
type Event struct {
	Subject string
	ID      string
	Data    []byte
}

// Events publishes and consumes JetStream events.
type Events struct {
	client *gokitnats.Client
	logger *slog.Logger

	// nakDelay is how long a failed message waits before redelivery. Tests
	// shorten it.
	nakDelay time.Duration

	mu       sync.Mutex
	declared map[string]bool
	handlers map[string]func(context.Context, Event) error
	started  bool
	cctxs    []jetstream.ConsumeContext
	inflight sync.WaitGroup
}

// NewEvents creates or updates every declared stream and durable consumer.
func NewEvents(ctx context.Context, client *gokitnats.Client, logger *slog.Logger, s EventsSettings) (*Events, error) {
	e := &Events{
		client:   client,
		logger:   logger,
		nakDelay: defaultNakDelay,
		declared: make(map[string]bool),
		handlers: make(map[string]func(context.Context, Event) error),
	}
	for _, st := range s.Streams {
		maxAge := st.MaxAge
		if maxAge <= 0 {
			maxAge = defaultStreamMaxAge
		}
		cfg := jetstream.StreamConfig{
			Name:      st.Name,
			Subjects:  st.Subjects,
			Retention: jetstream.LimitsPolicy,
			Storage:   jetstream.FileStorage,
			MaxAge:    maxAge,
		}
		if _, err := client.CreateStream(ctx, cfg); err != nil {
			return nil, err
		}
	}
	for _, c := range s.Consumers {
		maxDeliver := c.MaxDeliver
		if maxDeliver <= 0 {
			maxDeliver = defaultMaxDeliver
		}
		ackWait := c.AckWait
		if ackWait <= 0 {
			ackWait = defaultAckWait
		}
		cfg := jetstream.ConsumerConfig{
			Name:          c.Name,
			Durable:       c.Name,
			FilterSubject: c.Subject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: jetstream.DeliverAllPolicy,
			MaxDeliver:    maxDeliver,
			AckWait:       ackWait,
		}
		if _, err := client.CreateConsumer(ctx, c.Stream, cfg); err != nil {
			return nil, err
		}
		e.declared[c.Name] = true
	}
	return e, nil
}

// Publish sends v as JSON on subject. msgID, when set, deduplicates within the
// stream's duplicate window.
func (e *Events) Publish(ctx context.Context, subject string, v any, msgID string) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", subject, err)
	}
	if _, err := e.client.Publish(ctx, subject, data, msgID); err != nil {
		return err
	}
	return nil
}

// Subscribe registers h for a declared consumer. Call it before Run.
//
// h returning nil acks the message, an error wrapping ErrPermanent terminates
// it, and any other error asks for redelivery after a delay.
func (e *Events) Subscribe(consumer string, h func(ctx context.Context, ev Event) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.declared[consumer] {
		return fmt.Errorf("consumer %s not declared in %s", consumer, eventsDeclarationHint)
	}
	if _, ok := e.handlers[consumer]; ok {
		return fmt.Errorf("consumer %s already subscribed", consumer)
	}
	if e.started {
		return fmt.Errorf("consumer %s: Subscribe after start", consumer)
	}
	e.handlers[consumer] = h
	return nil
}

// start begins consuming for every subscription.
func (e *Events) start(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.started = true
	for name, h := range e.handlers {
		cc, err := e.client.ConsumeContext(name, e.dispatch(name, h))
		if err != nil {
			return err
		}
		e.cctxs = append(e.cctxs, cc)
	}
	return nil
}

func (e *Events) dispatch(consumer string, h func(context.Context, Event) error) func(context.Context, jetstream.Msg) {
	return func(ctx context.Context, msg jetstream.Msg) {
		e.inflight.Add(1)
		defer e.inflight.Done()

		ev := Event{Subject: msg.Subject(), ID: msg.Headers().Get(msgIDHeader), Data: msg.Data()}
		err := h(ctx, ev)
		switch {
		case err == nil:
			if aerr := msg.Ack(); aerr != nil {
				e.logger.Warn("event ack failed", "consumer", consumer, "subject", ev.Subject, "id", ev.ID, "error", aerr)
			}
		case errors.Is(err, ErrPermanent):
			e.logger.Error("event failed permanently", "consumer", consumer, "subject", ev.Subject, "id", ev.ID, "error", err)
			if terr := msg.Term(); terr != nil {
				e.logger.Warn("event term failed", "consumer", consumer, "error", terr)
			}
		default:
			var delivered uint64
			if md, merr := msg.Metadata(); merr == nil {
				delivered = md.NumDelivered
			}
			e.logger.Warn("event failed, will retry", "consumer", consumer, "subject", ev.Subject, "id", ev.ID, "delivery", delivered, "error", err)
			if nerr := msg.NakWithDelay(e.nakDelay); nerr != nil {
				e.logger.Warn("event nak failed", "consumer", consumer, "error", nerr)
			}
		}
	}
}

// stop drains every consume context and waits for in-flight handlers, bounded
// by ctx. It is safe to call when start never ran or failed part way.
func (e *Events) stop(ctx context.Context) error {
	e.mu.Lock()
	cctxs := e.cctxs
	e.cctxs = nil
	e.mu.Unlock()

	for _, cc := range cctxs {
		cc.Drain()
	}
	for _, cc := range cctxs {
		select {
		case <-cc.Closed():
		case <-ctx.Done():
			return fmt.Errorf("drain events: %w", ctx.Err())
		}
	}
	done := make(chan struct{})
	go func() {
		e.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for event handlers: %w", ctx.Err())
	}
}
