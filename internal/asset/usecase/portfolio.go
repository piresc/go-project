package usecase

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/piresc/gokit/idempotency"

	"github.com/piresc/go-project/internal/asset/domain"
)

// PortfolioUsecase manages user portfolios.
type PortfolioUsecase struct {
	repo PortfolioRepository
}

// NewPortfolioUsecase builds a PortfolioUsecase on top of its repository port.
func NewPortfolioUsecase(repo PortfolioRepository) *PortfolioUsecase {
	return &PortfolioUsecase{repo: repo}
}

// EnsurePortfolio creates the user's portfolio unless it already exists, so
// redelivered user.created events are harmless.
func (u *PortfolioUsecase) EnsurePortfolio(ctx context.Context, userID string) error {
	if _, err := uuid.Parse(userID); err != nil {
		return fmt.Errorf("user id %q: %w", userID, ErrInvalidInput)
	}
	res, err := idempotency.Check(ctx, userID, u.repo.Get, ErrNotFound, "ensure portfolio")
	if err != nil {
		return err
	}
	if res.Found {
		return nil
	}
	if err := u.repo.Create(ctx, userID); err != nil {
		return fmt.Errorf("create portfolio: %w", err)
	}
	return nil
}

// Get returns the user's portfolio, or ErrNotFound.
func (u *PortfolioUsecase) Get(ctx context.Context, userID string) (*domain.Portfolio, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, fmt.Errorf("user id %q: %w", userID, ErrInvalidInput)
	}
	return u.repo.Get(ctx, userID)
}
