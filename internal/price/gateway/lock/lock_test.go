package lock

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/piresc/gokit/config"
	gokitredis "github.com/piresc/gokit/redis"
	"github.com/piresc/gokit/redislock"
)

func TestLocker_HeldThenReleased(t *testing.T) {
	mini := miniredis.RunT(t)
	port, err := strconv.Atoi(mini.Port())
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	client, err := gokitredis.NewClient(config.RedisConfig{Host: mini.Host(), Port: port})
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	t.Cleanup(func() { _ = client.GetClient().Close() })
	inner, err := redislock.NewLocker(client, redislock.Config{Prefix: "price-service", TTL: 10 * time.Second})
	if err != nil {
		t.Fatalf("locker: %v", err)
	}
	l := New(inner)
	ctx := context.Background()

	first, err := l.Acquire(ctx, "refresh")
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if _, err := l.Acquire(ctx, "refresh"); !errors.Is(err, redislock.ErrLockUnavailable) {
		t.Fatalf("second Acquire = %v, want ErrLockUnavailable", err)
	}
	if err := first.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	again, err := l.Acquire(ctx, "refresh")
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	_ = again.Release(ctx)
}
