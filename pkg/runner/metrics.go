package runner

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

type SystemMetrics struct {
	CPUPercent        float64
	UnusedMemoryBytes uint64
}

type MetricsCollector struct {
	lastCallTime time.Time
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		lastCallTime: time.Now(),
	}
}

func (mc *MetricsCollector) Collect() (SystemMetrics, error) {
	metrics := SystemMetrics{}

	now := time.Now()
	interval := now.Sub(mc.lastCallTime)
	mc.lastCallTime = now
	if interval > 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}

	// Get system-wide CPU usage
	cpuPercents, err := cpu.Percent(interval, false)
	if err != nil {
		return metrics, fmt.Errorf("failed to get CPU stats: %w", err)
	}
	if len(cpuPercents) > 0 {
		metrics.CPUPercent = cpuPercents[0]
	}

	// Get system-wide memory stats
	memStats, err := mem.VirtualMemory()
	if err != nil {
		return metrics, fmt.Errorf("failed to get memory stats: %w", err)
	}
	metrics.UnusedMemoryBytes = memStats.Available

	return metrics, nil
}
