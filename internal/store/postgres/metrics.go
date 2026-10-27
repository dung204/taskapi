package postgres

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type PoolCollector struct {
	pool *pgxpool.Pool

	acquiredConns, idleConns, maxConns *prometheus.Desc
	acquires, acquireSeconds           *prometheus.Desc
	emptyAcquires, canceledAcquires    *prometheus.Desc
	newConns, destroyedConns           *prometheus.Desc
}

func NewPoolCollector(pool *pgxpool.Pool) *PoolCollector {
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc("db_pool_"+name, help, labels, nil)
	}

	return &PoolCollector{
		pool:             pool,
		acquiredConns:    desc("acquired_connections", "Connections currently in use."),
		idleConns:        desc("idle_connections", "Connections currently idle."),
		maxConns:         desc("max_connections", "Maximum pool size."),
		acquires:         desc("acquires_total", "Successful connection acquires."),
		acquireSeconds:   desc("acquire_seconds_total", "Total time spent in successful acquires."),
		emptyAcquires:    desc("empty_acquires_total", "Acquires that had to wait because the pool was empty."),
		canceledAcquires: desc("canceled_acquires_total", "Acquires canceled by their context."),
		newConns:         desc("new_connections_total", "New connections opened."),
		destroyedConns:   desc("destroyed_connections_total", "Connections closed by the pool.", "reason"),
	}
}

// Collect implements [prometheus.Collector].
func (c *PoolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	gauge, counter := prometheus.GaugeValue, prometheus.CounterValue

	ch <- prometheus.MustNewConstMetric(c.acquiredConns, gauge, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.idleConns, gauge, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.maxConns, gauge, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.acquires, counter, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquireSeconds, counter, s.AcquireDuration().Seconds())
	ch <- prometheus.MustNewConstMetric(c.emptyAcquires, counter, float64(s.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.canceledAcquires, counter, float64(s.CanceledAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.newConns, counter, float64(s.NewConnsCount()))
	ch <- prometheus.MustNewConstMetric(c.destroyedConns, counter, float64(s.MaxLifetimeDestroyCount()), "max_lifetime")
	ch <- prometheus.MustNewConstMetric(c.destroyedConns, counter, float64(s.MaxIdleDestroyCount()), "max_idle")
}

// Describe implements [prometheus.Collector].
func (c *PoolCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(c, ch)
}
