package messaging

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	gokitnats "github.com/piresc/gokit/nats"

	"github.com/piresc/go-project/services/user-service/internal/domain"
)

func TestPublishUserCreated(t *testing.T) {
	opts := natstest.DefaultTestOptions
	opts.Port = natsserver.RANDOM_PORT
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)

	client, err := gokitnats.NewClient(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats client: %v", err)
	}
	t.Cleanup(func() { client.Close(context.Background()) })

	sub, err := client.Conn().SubscribeSync("user.created")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	publisher := NewNATSPublisher(client)
	event := domain.UserCreatedEvent{UserID: "user-1", Email: "test@example.com", Role: "investor"}
	if err := publisher.PublishUserCreated(context.Background(), event); err != nil {
		t.Fatalf("PublishUserCreated: %v", err)
	}

	msg, err := sub.NextMsg(5 * time.Second)
	if err != nil {
		t.Fatalf("no message received: %v", err)
	}
	var raw map[string]string
	if err := json.Unmarshal(msg.Data, &raw); err != nil {
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
