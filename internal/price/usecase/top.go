package usecase

import (
	"context"
	"fmt"
	"slices"

	"github.com/piresc/go-project/internal/price/domain"
)

// TopUsecase ranks the cached quotes.
type TopUsecase struct {
	cache QuoteCache
}

// NewTopUsecase builds a TopUsecase.
func NewTopUsecase(cache QuoteCache) *TopUsecase {
	return &TopUsecase{cache: cache}
}

// Top returns the n quotes with the highest ChangePct, best first. It reads the
// cache only, so it keeps working while the market source is down. An empty
// cache gives an empty, non-nil slice.
func (u *TopUsecase) Top(ctx context.Context, n int) ([]domain.Quote, error) {
	cached, err := u.cache.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("read quote cache: %w", err)
	}
	sorted := slices.Clone(cached)
	slices.SortStableFunc(sorted, func(a, b domain.Quote) int {
		switch {
		case a.ChangePct > b.ChangePct:
			return -1
		case a.ChangePct < b.ChangePct:
			return 1
		default:
			return 0
		}
	})
	if n < 0 {
		n = 0
	}
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	if sorted == nil {
		sorted = []domain.Quote{}
	}
	return sorted, nil
}
