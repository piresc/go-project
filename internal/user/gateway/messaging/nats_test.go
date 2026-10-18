package messaging

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go/jetstream"
	gokitnats "github.com/piresc/gokit/nats"

	"github.com/piresc/go-project/internal/user/domain"
	"github.com/piresc/go-project/pkg/platform"
)

func TestPublishUserCreated(t *testing.T) {
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

	ctx := context.Background()
	events, err := platform.NewEvents(ctx, client, slog.Default(), platform.EventsSettings{
		Streams:   []platform.StreamSettings{{Name: "USERS", Subjects: []string{"user.>"}}},
		Consumers: []platform.ConsumerSettings{{Name: "test-user-created", Stream: "USERS", Subject: "user.created"}},
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	consumer, err := client.CreateConsumer(ctx, "USERS", jetstream.ConsumerConfig{
		Name:          "probe",
		Durable:       "probe",
		FilterSubject: "user.created",
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("consumer: %v", err)
	}

	publisher := NewNATSPublisher(events)
	event := domain.UserCreatedEvent{UserID: "user-1", Email: "test@example.com", Role: "investor"}
	if err := publisher.PublishUserCreated(ctx, event); err != nil {
		t.Fatalf("PublishUserCreated: %v", err)
	}

	msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
	if err != nil {
		t.Fatalf("no message received: %v", err)
	}
	if id := msg.Headers().Get("Nats-Msg-Id"); id != "user-1" {
		t.Fatalf("Nats-Msg-Id = %q, want user-1", id)
	}
	var raw map[string]string
	if err := json.Unmarshal(msg.Data(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]string{"user_id": "user-1", "email": "test@example.com", "role": "investor"}
	if len(raw) != len(want) {
		t.Fatalf("unexpected payload: %v", raw)
	}
	for k, v := range want {
		if raw[k] != v {
			t.Fatalf("payload[%s] = %q, want %q", k, raw[k], v)
		}
	}
}
