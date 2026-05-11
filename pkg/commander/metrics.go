package commander

import (
	"context"

	"github.com/Nesquiko/servermore/pkg/caching"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type CommanderMetrics struct {
	runnersGauge         metric.Int64ObservableGauge
	cachedInstancesGauge metric.Int64ObservableGauge

	registrations []metric.Registration
}

func NewCommanderMetrics(db CommanderDB, cache caching.RoutingCache) (*CommanderMetrics, error) {
	meter := otel.GetMeterProvider().Meter("github.com/Nesquiko/servermore/commander")
	runnersGauge, err := meter.Int64ObservableGauge("commander_runners_gauge")
	if err != nil {
		return nil, err
	}
	cachedInstancesGauge, err := meter.Int64ObservableGauge("commander_cached_instances_gauge")
	if err != nil {
		return nil, err
	}

	metrics := &CommanderMetrics{
		runnersGauge:         runnersGauge,
		cachedInstancesGauge: cachedInstancesGauge,
	}

	registration, err := meter.RegisterCallback(
		func(ctx context.Context, obs metric.Observer) error {
			runners, err := db.GetAllRunners(ctx)
			if err == nil {
				obs.ObserveInt64(runnersGauge, int64(len(runners)))
			}

			cachedInstances, cachedErr := cache.CachedInstancesCount(ctx)
			if cachedErr == nil {
				obs.ObserveInt64(cachedInstancesGauge, int64(cachedInstances))
			}

			if err != nil {
				return err
			}
			return cachedErr
		},
		runnersGauge,
		cachedInstancesGauge,
	)
	if err != nil {
		return nil, err
	}
	metrics.registrations = append(metrics.registrations, registration)

	return metrics, nil
}

func (m *CommanderMetrics) Close() {
	if m == nil {
		return
	}
	for _, reg := range m.registrations {
		if reg != nil {
			_ = reg.Unregister()
		}
	}
}
