// Package eventhandler adapts JetStream events to asset-service use cases.
package eventhandler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/piresc/go-project/internal/asset/usecase"
	"github.com/piresc/go-project/pkg/platform"
)

// PortfolioEnsurer creates a user's portfolio when missing.
type PortfolioEnsurer interface {
	EnsurePortfolio(ctx context.Context, userID string) error
}

// UserCreated handles the user.created event.
type UserCreated struct {
	uc PortfolioEnsurer
}

// NewUserCreated builds the user.created handler.
func NewUserCreated(uc PortfolioEnsurer) *UserCreated {
	return &UserCreated{uc: uc}
}

type userCreatedPayload struct {
	UserID string `json:"user_id"`
}

// Handle decodes the event and ensures the portfolio exists. Malformed events
// are permanent failures; anything else is retried.
func (h *UserCreated) Handle(ctx context.Context, ev platform.Event) error {
	var p userCreatedPayload
	if err := json.Unmarshal(ev.Data, &p); err != nil {
		return fmt.Errorf("decode user.created: %w: %w", err, platform.ErrPermanent)
	}
	if err := h.uc.EnsurePortfolio(ctx, p.UserID); err != nil {
		if errors.Is(err, usecase.ErrInvalidInput) {
			return fmt.Errorf("user.created: %w: %w", err, platform.ErrPermanent)
		}
		return err
	}
	return nil
}
