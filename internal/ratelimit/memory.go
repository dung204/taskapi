package ratelimit

import (
	"context"
	"maps"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type MemoryLimiter struct {
	mu      sync.Mutex
	clients map[string]*client
	limit   rate.Limit
	burst   int
	ttl     time.Duration
	now     func() time.Time
}

func NewMemoryLimiter(limit rate.Limit, burst int) *MemoryLimiter {
	return &MemoryLimiter{
		clients: make(map[string]*client),
		limit:   limit,
		burst:   burst,
		ttl:     5 * time.Minute,
		now:     time.Now,
	}
}

func (l *MemoryLimiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	c, found := l.clients[key]
	if !found {
		c = &client{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.clients[key] = c
	}
	c.lastSeen = now

	resv := c.limiter.ReserveN(now, 1)
	if delay := resv.DelayFrom(now); delay > 0 {
		resv.CancelAt(now)
		return false, delay
	}

	return true, 0
}

func (l *MemoryLimiter) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			l.cleanup(t)
		}
	}
}

func (l *MemoryLimiter) cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	maps.DeleteFunc(l.clients, func(_ string, c *client) bool {
		return now.Sub(c.lastSeen) > l.ttl
	})
}

func (l *MemoryLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.clients)
}
