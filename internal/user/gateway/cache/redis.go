// Package cache provides the Redis-backed user profile cache.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/piresc/gokit/redis"
	goredis "github.com/redis/go-redis/v9"

	"github.com/piresc/go-project/internal/user/domain"
	"github.com/piresc/go-project/internal/user/usecase"
)

const (
	userKeyPrefix = "user:"
	userCacheTTL  = 5 * time.Minute
)

var _ usecase.UserCache = (*RedisCache)(nil)

// RedisCache stores user profiles.
type RedisCache struct {
	client *goredis.Client
}

// NewRedisCache creates a Redis-backed cache from a gokit client.
func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client.GetClient()}
}

// GetUser returns a cached user if present.
func (c *RedisCache) GetUser(ctx context.Context, userID string) (*domain.User, bool, error) {
	value, err := c.client.Get(ctx, userKeyPrefix+userID).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var user domain.User
	if err := json.Unmarshal([]byte(value), &user); err != nil {
		return nil, false, err
	}
	return &user, true, nil
}

// SetUser caches a user profile.
func (c *RedisCache) SetUser(ctx context.Context, user *domain.User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, userKeyPrefix+user.ID, data, userCacheTTL).Err()
}

// DeleteUser removes a cached user profile.
func (c *RedisCache) DeleteUser(ctx context.Context, userID string) error {
	return c.client.Del(ctx, userKeyPrefix+userID).Err()
}
