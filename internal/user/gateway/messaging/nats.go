// Package messaging publishes user-service events to JetStream.
package messaging

import (
	"context"
	"fmt"

	"github.com/piresc/go-project/internal/user/domain"
	"github.com/piresc/go-project/internal/user/usecase"
	"github.com/piresc/go-project/pkg/platform"
)

const userCreatedSubject = "user.created"

var _ usecase.EventPublisher = (*NATSPublisher)(nil)

// NATSPublisher publishes user events through platform Events.
type NATSPublisher struct {
	events *platform.Events
}

// NewNATSPublisher creates a JetStream-backed publisher.
func NewNATSPublisher(events *platform.Events) *NATSPublisher {
	return &NATSPublisher{events: events}
}

// PublishUserCreated publishes a user.created event. The user ID is the
// message ID, so JetStream drops duplicate publishes.
func (p *NATSPublisher) PublishUserCreated(ctx context.Context, event domain.UserCreatedEvent) error {
	if err := p.events.Publish(ctx, userCreatedSubject, event, event.UserID); err != nil {
		return fmt.Errorf("publish user.created event: %w", err)
	}
	return nil
}
