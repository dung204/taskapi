package worker

import (
	"context"
	"log/slog"
	"time"
)

type CleanupTask struct {
	Name string
	Run  func(ctx context.Context, now time.Time, limit int32) (int64, error)
}

type Cleanup struct {
	interval time.Duration
	logger   *slog.Logger
	now      func() time.Time
	tasks    []CleanupTask
}

var _ Runner = (*Cleanup)(nil)

type CleanupDeps struct {
	Interval time.Duration
	Logger   *slog.Logger
	Now      func() time.Time
}

func NewCleanup(deps CleanupDeps, tasks ...CleanupTask) *Cleanup {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &Cleanup{
		deps.Interval,
		deps.Logger,
		deps.Now,
		tasks,
	}
}

// Run implements [Runner].
func (c *Cleanup) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		// run tasks immediately before ticks
		for _, t := range c.tasks {
			c.runTask(ctx, t)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

const (
	cleanupBatchSize  = 1000
	cleanupMaxBatches = 100
)

func (c *Cleanup) runTask(ctx context.Context, t CleanupTask) {
	var total int64
	for range cleanupMaxBatches {
		if ctx.Err() != nil {
			return
		}

		deleted, err := t.Run(ctx, c.now(), cleanupBatchSize)
		if err != nil {
			c.logger.ErrorContext(ctx, "cleanup failed", "task", t.Name, "error", err)
			return
		}

		total += deleted

		if deleted < cleanupBatchSize {
			break
		}
	}

	if total > 0 {
		c.logger.InfoContext(ctx, "cleanup done", "task", t.Name, "deleted", total)
	}
}
