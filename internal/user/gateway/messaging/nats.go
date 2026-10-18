// Package messaging publishes user-service events to NATS.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	gokitnats "github.com/piresc/gokit/nats"

	"github.com/piresc/go-project/internal/user/domain"
	"github.com/piresc/go-project/internal/user/usecase"
)

const userCreatedSubject = "user.created"

var _ usecase.EventPublisher = (*NATSPublisher)(nil)

// NATSPublisher publishes user events over core NATS.
type NATSPublisher struct {
	conn *nats.Conn
}

// NewNATSPublisher creates a NATS-backed publisher.
func NewNATSPublisher(client *gokitnats.Client) *NATSPublisher {
	return &NATSPublisher{conn: client.Conn()}
}

// PublishUserCreated publishes a user.created event.
func (p *NATSPublisher) PublishUserCreated(_ context.Context, event domain.UserCreatedEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal user.created event: %w", err)
	}
	if err := p.conn.Publish(userCreatedSubject, data); err != nil {
		return fmt.Errorf("publish user.created event: %w", err)
	}
	return nil
}
