package usecase

import (
	"context"

	"github.com/piresc/go-project/internal/asset/domain"
)

// AssetRepository is the persistence port for assets. Placeholder: add methods.
type AssetRepository interface{}

// PortfolioRepository is the persistence port for portfolios.
type PortfolioRepository interface {
	// Get returns ErrNotFound when the user has no portfolio.
	Get(ctx context.Context, userID string) (*domain.Portfolio, error)
	// Create is idempotent: creating an existing portfolio is a no-op.
	Create(ctx context.Context, userID string) error
}
