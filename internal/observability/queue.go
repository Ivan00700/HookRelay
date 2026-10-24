package observability

import (
	"context"
	"time"

	"hookrelay/internal/domain"

	"github.com/prometheus/client_golang/prometheus"
)

type CountsReader func(ctx context.Context) (map[domain.DeliveryStatus]int64, error)

type queueCollector struct {
	readCounts CountsReader
	events     *prometheus.Desc
	queue      *prometheus.Desc
	databaseUp *prometheus.Desc
}

func newQueueCollector(readCounts CountsReader) *queueCollector {
	return &queueCollector{
		readCounts: readCounts,
		events:     prometheus.NewDesc("hookrelay_events", "Current events in PostgreSQL by status.", []string{"status"}, nil),
		queue:      prometheus.NewDesc("hookrelay_queue_events", "Pending and processing events in PostgreSQL, including delayed retries.", nil, nil),
		databaseUp: prometheus.NewDesc("hookrelay_database_up", "Whether the last queue collection succeeded.", nil, nil),
	}
}

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.events
	ch <- c.queue
	ch <- c.databaseUp
}

// читаем состояние при сборе метрик - перезапуск процесса не обнуляет очередь в БД
func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if c.readCounts == nil {
		ch <- prometheus.MustNewConstMetric(c.databaseUp, prometheus.GaugeValue, 0)
		return
	}
	counts, err := c.readCounts(ctx)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(c.databaseUp, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.databaseUp, prometheus.GaugeValue, 1)
	for _, status := range []domain.DeliveryStatus{domain.DeliveryStatusPending, domain.DeliveryStatusProcessing, domain.DeliveryStatusDelivered, domain.DeliveryStatusFailed} {
		ch <- prometheus.MustNewConstMetric(c.events, prometheus.GaugeValue, float64(counts[status]), string(status))
	}
	size := counts[domain.DeliveryStatusPending] + counts[domain.DeliveryStatusProcessing]
	ch <- prometheus.MustNewConstMetric(c.queue, prometheus.GaugeValue, float64(size))
}
