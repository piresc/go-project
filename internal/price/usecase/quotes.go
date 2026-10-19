package usecase

import (
	"context"
	"fmt"

	"github.com/piresc/go-project/internal/price/domain"
)

// QuotesUsecase looks up cached quotes by symbol.
type QuotesUsecase struct {
	cache QuoteCache
}

// NewQuotesUsecase builds a QuotesUsecase.
func NewQuotesUsecase(cache QuoteCache) *QuotesUsecase {
	return &QuotesUsecase{cache: cache}
}

// Get returns the cached quotes whose symbol is in symbols, in the order of
// symbols. Unknown symbols are left out and duplicates are collapsed. It reads
// the cache only, never the market. The result is never nil.
func (u *QuotesUsecase) Get(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	cached, err := u.cache.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("read quote cache: %w", err)
	}
	bySymbol := make(map[string]domain.Quote, len(cached))
	for _, q := range cached {
		bySymbol[q.Symbol] = q
	}
	out := make([]domain.Quote, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, sym := range symbols {
		if _, dup := seen[sym]; dup {
			continue
		}
		seen[sym] = struct{}{}
		if q, ok := bySymbol[sym]; ok {
			out = append(out, q)
		}
	}
	return out, nil
}
