package cache

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/piresc/gokit/config"
	gokitredis "github.com/piresc/gokit/redis"

	"github.com/piresc/go-project/services/user-service/internal/domain"
)

func newTestCache(t *testing.T) (*RedisCache, *miniredis.Miniredis) {
	t.Helper()
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
	return NewRedisCache(client), mini
}

func TestRedisCacheRoundTrip(t *testing.T) {
	c, mini := newTestCache(t)
	ctx := context.Background()
	user := &domain.User{ID: "user-1", Email: "test@example.com", DisplayName: "Tester", Role: "investor"}

	if err := c.SetUser(ctx, user); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	got, found, err := c.GetUser(ctx, user.ID)
	if err != nil || !found || got == nil || got.Email != user.Email || got.DisplayName != "Tester" {
		t.Fatalf("GetUser: found=%v user=%+v err=%v", found, got, err)
	}
	if ttl := mini.TTL("user:user-1"); ttl != 5*time.Minute {
		t.Fatalf("ttl = %v, want 5m", ttl)
	}
}

func TestRedisCacheMiss(t *testing.T) {
	c, _ := newTestCache(t)
	got, found, err := c.GetUser(context.Background(), "missing")
	if err != nil || found || got != nil {
		t.Fatalf("expected clean miss, got user=%+v found=%v err=%v", got, found, err)
	}
}

func TestRedisCacheDelete(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	user := &domain.User{ID: "user-2", Email: "b@example.com"}
	if err := c.SetUser(ctx, user); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	if err := c.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, found, _ := c.GetUser(ctx, user.ID); found {
		t.Fatal("expected user to be deleted")
	}
}
