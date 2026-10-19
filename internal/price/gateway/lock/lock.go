// Package lock adapts the gokit Redis locker to the usecase port.
package lock

import (
	"context"
	"errors"

	"github.com/piresc/gokit/redislock"

	"github.com/piresc/go-project/internal/price/usecase"
)

var _ usecase.Locker = (*Locker)(nil)

// Locker wraps *redislock.Locker. Acquire reports a held lock as
// usecase.ErrLockHeld.
type Locker struct {
	inner *redislock.Locker
}

// New builds a Locker.
func New(inner *redislock.Locker) *Locker {
	return &Locker{inner: inner}
}

// Acquire takes the named lock.
func (l *Locker) Acquire(ctx context.Context, key string) (usecase.Lock, error) {
	lk, err := l.inner.Acquire(ctx, key)
	if errors.Is(err, redislock.ErrLockUnavailable) {
		return nil, usecase.ErrLockHeld
	}
	if err != nil {
		return nil, err
	}
	return lk, nil
}
