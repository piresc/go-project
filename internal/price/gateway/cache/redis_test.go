package cache

import (
	"context"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/piresc/gokit/config"
	gokitredis "github.com/piresc/gokit/redis"

	"github.com/piresc/go-project/internal/price/domain"
)

func newTestCache(t *testing.T) (*Redis, *miniredis.Miniredis) {
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
	return NewRedis(client), mini
}

func TestRedis_RoundTripWithoutTTL(t *testing.T) {
	c, mini := newTestCache(t)
	ctx := context.Background()
	in := []domain.Quote{{Symbol: "AAPL", Price: 190.5, ChangePct: 1.2}, {Symbol: "MSFT", Price: 410, ChangePct: -0.3}}

	if err := c.Set(ctx, in); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 2 || got[0] != in[0] || got[1] != in[1] {
		t.Fatalf("got %+v, want %+v", got, in)
	}
	if ttl := mini.TTL("prices:quotes"); ttl != 0 {
		t.Fatalf("ttl = %v, want none so stale data survives an outage", ttl)
	}
}

func TestRedis_MissIsEmpty(t *testing.T) {
	c, _ := newTestCache(t)
	got, err := c.Get(context.Background())
	if err != nil {
		t.Fatalf("Get on empty cache: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
}

func TestRedis_CorruptValueErrors(t *testing.T) {
	c, mini := newTestCache(t)
	if err := mini.Set("prices:quotes", "not json"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := c.Get(context.Background()); err == nil {
		t.Fatal("want decode error")
	}
}
