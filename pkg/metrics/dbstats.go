package metrics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CollectDBPoolStats периодически снимает срез состояния пула соединений
// с PostgreSQL (idle/used/max) и пишет его в DBPoolConnections.
//
// pgxpool не push-ит свою статистику никуда сам — единственный способ её
// получить, это спросить pool.Stat() в моменте. Poll-луп с тикером —
// самый простой способ превратить такой "снимок по запросу" в метрику,
// которую Prometheus сможет забирать раз в свой scrape interval.
//
// Останавливается сам, когда отменяется ctx (тот же контекст, что
// используется для graceful shutdown в cmd/server и cmd/worker) — поэтому
// вызывающему коду достаточно один раз запустить это в go-рутине и не
// думать об остановке отдельно.
func CollectDBPoolStats(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stat := pool.Stat()
			DBPoolConnections.WithLabelValues("idle").Set(float64(stat.IdleConns()))
			DBPoolConnections.WithLabelValues("used").Set(float64(stat.AcquiredConns()))
			DBPoolConnections.WithLabelValues("max").Set(float64(stat.MaxConns()))
		}
	}
}
