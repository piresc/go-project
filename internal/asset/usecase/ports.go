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
	// UpsertHoldingCapped sets the quantity held of symbol. The portfolio must
	// exist. In one transaction it returns ErrTooManyHoldings when symbol is
	// new and the user already holds limit holdings.
	UpsertHoldingCapped(ctx context.Context, userID, symbol string, qty float64, limit int) error
	// DeleteHolding returns ErrNotFound when the user does not hold symbol.
	DeleteHolding(ctx context.Context, userID, symbol string) error
	// ListHoldings returns the user's holdings ordered by symbol.
	ListHoldings(ctx context.Context, userID string) ([]domain.Holding, error)
}

// PriceSource supplies current quotes. Symbols without a quote are omitted.
type PriceSource interface {
	Quotes(ctx context.Context, symbols []string) ([]domain.Quote, error)
}
