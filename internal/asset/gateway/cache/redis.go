// Package cache holds the Redis cache adapter.
package cache

import "github.com/piresc/gokit/redis"

// Redis caches asset data in Redis. Placeholder: no operations yet.
type Redis struct {
	client *redis.Client
}

// NewRedis builds a Redis cache on the given client.
func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}
