package platform

import (
	"fmt"
	"time"

	"github.com/piresc/gokit/redislock"
)

const defaultLockTTL = 30 * time.Second

// LockSettings tunes the distributed Locker, under "service.lock".
type LockSettings struct {
	// TTL is how long a lock lives if never released. Defaults to 30s.
	TTL time.Duration `yaml:"ttl" mapstructure:"ttl"`
}

// initLocker builds Locker when Redis is connected. Keys are prefixed with the
// service name and Acquire never retries: a held lock is reported at once.
func (a *App[S]) initLocker(name string) error {
	if a.Redis == nil {
		return nil
	}
	ttl := a.Settings.Lock.TTL
	if ttl <= 0 {
		ttl = defaultLockTTL
	}
	l, err := redislock.NewLocker(a.Redis, redislock.Config{Prefix: name, TTL: ttl, RetryAttempts: 0})
	if err != nil {
		return fmt.Errorf("locker: %w", err)
	}
	a.Locker = l
	return nil
}
