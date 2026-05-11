package runner

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type RunnerMetrics struct {
	prepareDownloadDur  metric.Float64Histogram
	prepareStartDur     metric.Float64Histogram
	invokeQueueWait     metric.Float64Histogram
	invokeGuestDuration metric.Float64Histogram
	instanceQueueDepth  metric.Int64ObservableGauge
	instancesActive     metric.Int64ObservableGauge
	registrations       []metric.Registration
}

func NewRunnerMetrics(instances *InstancesStates) (*RunnerMetrics, error) {
	meter := otel.GetMeterProvider().Meter("github.com/Nesquiko/servermore/runner")

	prepareDownloadDur, err := meter.Float64Histogram("runner_prepare_download_duration_seconds")
	if err != nil {
		return nil, err
	}
	prepareStartDur, err := meter.Float64Histogram("runner_prepare_start_duration_seconds")
	if err != nil {
		return nil, err
	}
	invokeQueueWait, err := meter.Float64Histogram("runner_invoke_queue_wait_seconds")
	if err != nil {
		return nil, err
	}
	invokeGuestDuration, err := meter.Float64Histogram("runner_invoke_guest_duration_seconds")
	if err != nil {
		return nil, err
	}
	instanceQueueDepth, err := meter.Int64ObservableGauge("runner_instance_queue_depth")
	if err != nil {
		return nil, err
	}
	instancesActive, err := meter.Int64ObservableGauge("runner_instances_active")
	if err != nil {
		return nil, err
	}

	metrics := &RunnerMetrics{
		prepareDownloadDur:  prepareDownloadDur,
		prepareStartDur:     prepareStartDur,
		invokeQueueWait:     invokeQueueWait,
		invokeGuestDuration: invokeGuestDuration,
		instanceQueueDepth:  instanceQueueDepth,
		instancesActive:     instancesActive,
	}

	registration, err := meter.RegisterCallback(
		func(ctx context.Context, obs metric.Observer) error {
			obs.ObserveInt64(instancesActive, int64(instances.ActiveInstancesCount()))

			for instanceID, depth := range instances.QueueDepths(0) {
				obs.ObserveInt64(
					instanceQueueDepth,
					int64(depth),
					metric.WithAttributes(attribute.String("instance_id", instanceID)),
				)
			}

			return nil
		},
		instancesActive,
		instanceQueueDepth,
	)
	if err != nil {
		return nil, err
	}
	metrics.registrations = append(metrics.registrations, registration)

	return metrics, nil
}

func (m *RunnerMetrics) Close() {
	if m == nil {
		return
	}
	for _, reg := range m.registrations {
		if reg != nil {
			_ = reg.Unregister()
		}
	}
}

func (m *RunnerMetrics) RecordPrepareDownload(ctx context.Context, took time.Duration) {
	if m == nil {
		return
	}
	m.prepareDownloadDur.Record(ctx, took.Seconds())
}

func (m *RunnerMetrics) RecordPrepareStart(ctx context.Context, took time.Duration) {
	if m == nil {
		return
	}
	m.prepareStartDur.Record(ctx, took.Seconds())
}

func (m *RunnerMetrics) RecordInvokeQueueWait(ctx context.Context, took time.Duration) {
	if m == nil {
		return
	}
	m.invokeQueueWait.Record(ctx, took.Seconds())
}

func (m *RunnerMetrics) RecordInvokeGuestDuration(ctx context.Context, took time.Duration) {
	if m == nil {
		return
	}
	m.invokeGuestDuration.Record(ctx, took.Seconds())
}
