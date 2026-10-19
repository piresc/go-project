package usecase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/piresc/gokit/idempotency"

	"github.com/piresc/go-project/internal/asset/domain"
)

// PortfolioUsecase manages user portfolios.
type PortfolioUsecase struct {
	repo   PortfolioRepository
	prices PriceSource
}

// NewPortfolioUsecase builds a PortfolioUsecase on top of its ports.
func NewPortfolioUsecase(repo PortfolioRepository, prices PriceSource) *PortfolioUsecase {
	return &PortfolioUsecase{repo: repo, prices: prices}
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

// MaxHoldings is the most holdings one portfolio may have. It matches the
// price-service batch limit so a valuation is always a single call.
const MaxHoldings = 50

const maxQuantity = 1e12

var symbolPattern = regexp.MustCompile(`^[A-Z0-9.\-]{1,10}$`)

// Holdings returns the user's holdings ordered by symbol; never nil.
func (u *PortfolioUsecase) Holdings(ctx context.Context, userID string) ([]domain.Holding, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, fmt.Errorf("user id %q: %w", userID, ErrInvalidInput)
	}
	holdings, err := u.repo.ListHoldings(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list holdings: %w", err)
	}
	if holdings == nil {
		holdings = []domain.Holding{}
	}
	return holdings, nil
}

// SetHolding sets the quantity held of symbol, creating the portfolio first
// if needed. Adding a new symbol beyond MaxHoldings returns ErrTooManyHoldings;
// the repository enforces the cap atomically, so concurrent adds cannot exceed it.
func (u *PortfolioUsecase) SetHolding(ctx context.Context, userID, symbol string, qty float64) (*domain.Holding, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, fmt.Errorf("user id %q: %w", userID, ErrInvalidInput)
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if !symbolPattern.MatchString(symbol) {
		return nil, fmt.Errorf("symbol %q: %w", symbol, ErrInvalidInput)
	}
	if math.IsNaN(qty) || math.IsInf(qty, 0) || qty <= 0 || qty > maxQuantity {
		return nil, fmt.Errorf("quantity %v: %w", qty, ErrInvalidInput)
	}
	if err := u.repo.Create(ctx, userID); err != nil {
		return nil, fmt.Errorf("create portfolio: %w", err)
	}
	if err := u.repo.UpsertHoldingCapped(ctx, userID, symbol, qty, MaxHoldings); err != nil {
		if errors.Is(err, ErrTooManyHoldings) {
			return nil, ErrTooManyHoldings
		}
		return nil, fmt.Errorf("upsert holding: %w", err)
	}
	return &domain.Holding{Symbol: symbol, Quantity: qty}, nil
}

// RemoveHolding deletes the holding, or returns ErrNotFound.
func (u *PortfolioUsecase) RemoveHolding(ctx context.Context, userID, symbol string) error {
	if _, err := uuid.Parse(userID); err != nil {
		return fmt.Errorf("user id %q: %w", userID, ErrInvalidInput)
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if !symbolPattern.MatchString(symbol) {
		return fmt.Errorf("symbol %q: %w", symbol, ErrInvalidInput)
	}
	return u.repo.DeleteHolding(ctx, userID, symbol)
}

// Value prices the user's holdings with one call to the price source. Symbols
// without a quote are reported in Unpriced. Any price source failure is
// wrapped as ErrPricesUnavailable.
func (u *PortfolioUsecase) Value(ctx context.Context, userID string) (*domain.Valuation, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, fmt.Errorf("user id %q: %w", userID, ErrInvalidInput)
	}
	if _, err := u.repo.Get(ctx, userID); err != nil {
		return nil, err
	}
	holdings, err := u.repo.ListHoldings(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list holdings: %w", err)
	}
	val := &domain.Valuation{Holdings: []domain.HoldingValue{}, Unpriced: []string{}}
	if len(holdings) == 0 {
		return val, nil
	}
	symbols := make([]string, len(holdings))
	for i, h := range holdings {
		symbols[i] = h.Symbol
	}
	quotes, err := u.prices.Quotes(ctx, symbols)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPricesUnavailable, err)
	}
	priceBySymbol := make(map[string]float64, len(quotes))
	for _, q := range quotes {
		priceBySymbol[q.Symbol] = q.Price
	}
	for _, h := range holdings {
		price, ok := priceBySymbol[h.Symbol]
		if !ok {
			val.Unpriced = append(val.Unpriced, h.Symbol)
			continue
		}
		value := h.Quantity * price
		val.Holdings = append(val.Holdings, domain.HoldingValue{Symbol: h.Symbol, Quantity: h.Quantity, Price: price, Value: value})
		val.Total += value
	}
	sort.Strings(val.Unpriced)
	return val, nil
}
