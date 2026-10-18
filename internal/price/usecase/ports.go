package usecase

import (
	"context"

	"github.com/piresc/go-project/internal/price/domain"
)

// QuoteSource fetches the current market quotes.
type QuoteSource interface {
	Quotes(ctx context.Context) ([]domain.Quote, error)
}

// QuoteCache stores the latest quotes. It has no expiry, so the last good
// quotes keep being served while the market source is down.
type QuoteCache interface {
	Set(ctx context.Context, quotes []domain.Quote) error
	// Get returns the cached quotes, or none when nothing is cached yet.
	Get(ctx context.Context) ([]domain.Quote, error)
}

// Lock is a held lock.
type Lock interface {
	Release(ctx context.Context) error
}

// Locker takes named distributed locks. Acquire returns
// redislock.ErrLockUnavailable when the lock is already held.
type Locker interface {
	Acquire(ctx context.Context, key string) (Lock, error)
}
