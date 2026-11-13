package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// newRedisClient builds the client without connecting: go-redis dials lazily
// and reconnects on its own, so a Redis that is down at startup is picked up
// once it is back. The error is only for an invalid URL.
func newRedisClient(redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	opts.ReadTimeout = 50 * time.Millisecond
	opts.WriteTimeout = 50 * time.Millisecond
	opts.DialTimeout = 200 * time.Millisecond
	opts.PoolTimeout = 100 * time.Millisecond
	opts.MaxRetries = -1 // disable retries
	opts.ContextTimeoutEnabled = true

	return redis.NewClient(opts), nil
}

func pingRedis(ctx context.Context, client *redis.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}
