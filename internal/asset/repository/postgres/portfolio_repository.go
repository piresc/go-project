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

// UpsertHoldingCapped sets the quantity held of symbol. The portfolio must
// exist. It locks the portfolio row for the transaction, so concurrent adds
// for one user are serialised and the count check cannot be raced. A new
// symbol when the user already holds limit holdings returns
// usecase.ErrTooManyHoldings.
func (r *PortfolioRepository) UpsertHoldingCapped(ctx context.Context, userID, symbol string, qty float64, limit int) (err error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var locked int
	if err = tx.GetContext(ctx, &locked, `SELECT 1 FROM portfolios WHERE user_id = $1 FOR UPDATE`, userID); err != nil {
		return fmt.Errorf("lock portfolio: %w", err)
	}
	var held bool
	if err = tx.GetContext(ctx, &held,
		`SELECT EXISTS (SELECT 1 FROM holdings WHERE user_id = $1 AND symbol = $2)`, userID, symbol); err != nil {
		return fmt.Errorf("check holding: %w", err)
	}
	if !held {
		var n int
		if err = tx.GetContext(ctx, &n, `SELECT count(*) FROM holdings WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("count holdings: %w", err)
		}
		if n >= limit {
			err = usecase.ErrTooManyHoldings
			return err
		}
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO holdings (user_id, symbol, quantity) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, symbol) DO UPDATE SET quantity = EXCLUDED.quantity, updated_at = NOW()`,
		userID, symbol, qty); err != nil {
		return fmt.Errorf("upsert holding: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// DeleteHolding removes the holding or returns usecase.ErrNotFound.
func (r *PortfolioRepository) DeleteHolding(ctx context.Context, userID, symbol string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM holdings WHERE user_id = $1 AND symbol = $2`, userID, symbol)
	if err != nil {
		return fmt.Errorf("delete holding: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete holding: %w", err)
	}
	if n == 0 {
		return usecase.ErrNotFound
	}
	return nil
}

// ListHoldings returns the user's holdings ordered by symbol.
func (r *PortfolioRepository) ListHoldings(ctx context.Context, userID string) ([]domain.Holding, error) {
	holdings := []domain.Holding{}
	err := r.db.SelectContext(ctx, &holdings,
		`SELECT symbol, quantity FROM holdings WHERE user_id = $1 ORDER BY symbol`, userID)
	if err != nil {
		return nil, fmt.Errorf("list holdings: %w", err)
	}
	return holdings, nil
}
