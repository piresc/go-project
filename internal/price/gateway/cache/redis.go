// Package cache holds the Redis cache adapter.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/piresc/gokit/redis"
	goredis "github.com/redis/go-redis/v9"

	"github.com/piresc/go-project/internal/price/domain"
	"github.com/piresc/go-project/internal/price/usecase"
)

const quotesKey = "prices:quotes"

var _ usecase.QuoteCache = (*Redis)(nil)

// Redis caches quotes in Redis.
type Redis struct {
	client *goredis.Client
}

// NewRedis builds a Redis cache on the given client.
func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client.GetClient()}
}

// Set stores the quotes. The key has no TTL, so the last good quotes survive a
// market outage.
func (c *Redis) Set(ctx context.Context, quotes []domain.Quote) error {
	data, err := json.Marshal(quotes)
	if err != nil {
		return fmt.Errorf("marshal quotes: %w", err)
	}
	return c.client.Set(ctx, quotesKey, data, 0).Err()
}

// Get returns the cached quotes, or none when nothing has been cached yet.
func (c *Redis) Get(ctx context.Context) ([]domain.Quote, error) {
	raw, err := c.client.Get(ctx, quotesKey).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var quotes []domain.Quote
	if err := json.Unmarshal(raw, &quotes); err != nil {
		return nil, fmt.Errorf("decode cached quotes: %w", err)
	}
	return quotes, nil
}
