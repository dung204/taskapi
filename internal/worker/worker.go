package worker

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/dung204/taskapi/internal/task"
)

func Run(ctx context.Context, s task.Store, l *slog.Logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	l.InfoContext(ctx, "worker started", "interval", interval.String())
	for {
		select {
		case <-ctx.Done():
			l.InfoContext(ctx, "worker stopped")
			return
		case t := <-ticker.C:
			scanOverdue(ctx, s, l, t)
		}
	}
}

func scanOverdue(ctx context.Context, s task.Store, l *slog.Logger, t time.Time) {
	defer func() {
		if rec := recover(); rec != nil {
			l.ErrorContext(ctx, "panic recovered",
				"panic", rec,
				"stack", string(debug.Stack()),
			)
		}
	}()

	markOverdueCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	now := time.Now()
	marked, err := s.MarkOverdue(markOverdueCtx, t)
	elapsed := time.Since(now)

	if err != nil {
		l.ErrorContext(ctx, "mark overdue failed", "error", err)
		return
	}

	level := slog.LevelDebug
	if marked > 0 {
		level = slog.LevelInfo
	}

	l.Log(ctx, level, "overdue scan completed",
		"count_updated", marked,
		"duration_ms", float64(elapsed)/float64(time.Millisecond),
	)
}
