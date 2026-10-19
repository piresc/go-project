package usecase

import "errors"

// ErrLockHeld is returned by a Locker when the lock is already held.
var ErrLockHeld = errors.New("lock held")
