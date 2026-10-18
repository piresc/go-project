package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/piresc/gokit/redislock"
)

func TestLocker_AcquireOnce(t *testing.T) {
	mr := newMiniredis(t)
	writeConfig(t, redisYAML(mr))
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.Locker == nil {
		t.Fatal("Locker is nil with Redis configured")
	}

	l, err := a.Locker.Acquire(context.Background(), "refresh")
	if err != nil {
		t.Fatal(err)
	}
	if l.Key() != "svc:lock:refresh" {
		t.Fatalf("key = %q, want prefix = service name", l.Key())
	}
	if _, err := a.Locker.Acquire(context.Background(), "refresh"); !errors.Is(err, redislock.ErrLockUnavailable) {
		t.Fatalf("second acquire: want ErrLockUnavailable (no retries), got %v", err)
	}
	if err := l.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLocker_NilWithoutRedis(t *testing.T) {
	writeConfig(t, "")
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.Locker != nil {
		t.Fatal("Locker should be nil without Redis")
	}
}
