package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/dung204/taskapi/internal/store/postgres"
)

type closeInfraFunc = func()

type infra struct {
	pool   *pgxpool.Pool
	client *redis.Client
	checks []*check
}

// openInfra scaffolds DB pool & Redis client
func openInfra(ctx context.Context, cfg config, logger *slog.Logger, reg *prometheus.Registry) (infra, closeInfraFunc, error) {
	var stops []func()
	stopAll := func() {
		for _, stop := range slices.Backward(stops) {
			stop()
		}
	}

	connectDBStart := time.Now()
	pool, err := connectDB(ctx, cfg.db.url)
	connectDBElapsed := time.Since(connectDBStart)

	if err != nil {
		return infra{}, nil, fmt.Errorf("connect to database %q on %q: %w", cfg.db.name, cfg.db.host, err)
	}

	stops = append(stops, func() {
		pool.Close()
		logger.Info("database closed")
	})

	logger.Info("database connected",
		"max_open_conns", pool.Stat().MaxConns(),
		"connect_duration_ms", float64(connectDBElapsed)/float64(time.Millisecond),
		"host", cfg.db.host,
		"database", cfg.db.name,
	)
	reg.MustRegister(postgres.NewPoolCollector(pool))

	client, err := newRedisClient(cfg.redis.url)
	if err != nil {
		stopAll() // closes the pool opened above
		return infra{}, nil, err
	}
	stops = append(stops, func() {
		_ = client.Close()
		logger.Info("redis closed")
	})

	// Redis is fail-open: an unreachable Redis at startup is not fatal. The
	// client keeps the connection details and reconnects when Redis is back.
	connectRedisStart := time.Now()
	if err := pingRedis(ctx, client); err != nil {
		logger.Warn("redis unreachable, continuing without revocation checks",
			"error", err,
			"host", cfg.redis.host,
			"database", cfg.redis.dbName,
		)
	} else {
		logger.Info("redis connected",
			"connect_duration_ms", float64(time.Since(connectRedisStart))/float64(time.Millisecond),
			"host", cfg.redis.host,
			"database", cfg.redis.dbName,
		)
	}

	return infra{
		pool:   pool,
		client: client,
		checks: []*check{
			newCheck("postgres", true, pool.Ping),
			newCheck("redis", false, func(ctx context.Context) error {
				return client.Ping(ctx).Err()
			}),
		},
	}, stopAll, nil
}
