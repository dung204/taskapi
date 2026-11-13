package worker

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/dung204/taskapi/internal/task"
)

type OverdueScan struct {
	tasks    task.Store
	logger   *slog.Logger
	interval time.Duration
}

var _ Runner = (*OverdueScan)(nil)

func NewOverdueScan(tasks task.Store, logger *slog.Logger, interval time.Duration) *OverdueScan {
	return &OverdueScan{tasks, logger, interval}
}

// Run implements [worker].
func (w *OverdueScan) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.logger.InfoContext(ctx, "worker started", "interval", w.interval.String())
	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "worker stopped")
			return
		case t := <-ticker.C:
			w.scanOverdue(ctx, t)
		}
	}
}

func (w *OverdueScan) scanOverdue(ctx context.Context, t time.Time) {
	defer func() {
		if rec := recover(); rec != nil {
			w.logger.ErrorContext(ctx, "panic recovered",
				"panic", rec,
				"stack", string(debug.Stack()),
			)
		}
	}()

	markOverdueCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	now := time.Now()
	marked, err := w.tasks.MarkOverdue(markOverdueCtx, t)
	elapsed := time.Since(now)

	if err != nil {
		w.logger.ErrorContext(ctx, "mark overdue failed", "error", err)
		return
	}

	level := slog.LevelDebug
	if marked > 0 {
		level = slog.LevelInfo
	}

	w.logger.Log(ctx, level, "overdue scan completed",
		"count_updated", marked,
		"duration_ms", float64(elapsed)/float64(time.Millisecond),
	)
}
