// Package usecase holds price-service business logic.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/piresc/gokit/redislock"
)

const refreshLockKey = "refresh"

// RefreshUsecase fetches fresh quotes and caches them.
type RefreshUsecase struct {
	source QuoteSource
	cache  QuoteCache
	locker Locker
	log    *slog.Logger
}

// NewRefreshUsecase builds a RefreshUsecase.
func NewRefreshUsecase(source QuoteSource, cache QuoteCache, locker Locker, log *slog.Logger) *RefreshUsecase {
	return &RefreshUsecase{source: source, cache: cache, locker: locker, log: log}
}

// Refresh fetches the quotes and caches them under a lock, so runs do not
// overlap. A held lock means another run is in progress: Refresh skips and
// returns nil. A failed fetch leaves the cache as it was.
func (u *RefreshUsecase) Refresh(ctx context.Context) error {
	lock, err := u.locker.Acquire(ctx, refreshLockKey)
	if errors.Is(err, redislock.ErrLockUnavailable) {
		u.log.DebugContext(ctx, "price refresh already running, skipping")
		return nil
	}
	if err != nil {
		return fmt.Errorf("acquire refresh lock: %w", err)
	}
	defer func() {
		if rerr := lock.Release(context.WithoutCancel(ctx)); rerr != nil {
			u.log.WarnContext(ctx, "release refresh lock", "error", rerr)
		}
	}()

	quotes, err := u.source.Quotes(ctx)
	if err != nil {
		return fmt.Errorf("fetch quotes: %w", err)
	}
	if err := u.cache.Set(ctx, quotes); err != nil {
		return fmt.Errorf("cache quotes: %w", err)
	}
	return nil
}
