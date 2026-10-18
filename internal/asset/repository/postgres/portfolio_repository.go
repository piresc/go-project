package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/piresc/go-project/internal/asset/domain"
	"github.com/piresc/go-project/internal/asset/usecase"
)

// PortfolioRepository stores portfolios in Postgres.
type PortfolioRepository struct {
	db *sqlx.DB
}

// NewPortfolioRepository builds a PortfolioRepository on the given connection.
func NewPortfolioRepository(db *sqlx.DB) *PortfolioRepository {
	return &PortfolioRepository{db: db}
}

// Get returns the user's portfolio or usecase.ErrNotFound.
func (r *PortfolioRepository) Get(ctx context.Context, userID string) (*domain.Portfolio, error) {
	var p domain.Portfolio
	err := r.db.GetContext(ctx, &p, `SELECT user_id, created_at FROM portfolios WHERE user_id = $1`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, usecase.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get portfolio: %w", err)
	}
	return &p, nil
}

// Create inserts the portfolio; an existing row is left untouched.
func (r *PortfolioRepository) Create(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO portfolios (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, userID)
	if err != nil {
		return fmt.Errorf("insert portfolio: %w", err)
	}
	return nil
}
