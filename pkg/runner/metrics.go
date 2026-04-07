package runner

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ProcessMetrics struct {
	CPUPercent  float64
	MemoryBytes uint64
}

type MetricsCollector struct {
	mu sync.Mutex

	lastCPUTime   uint64
	lastCheckTime time.Time

	numCPU int
}

func NewMetricsCollector() *MetricsCollector {
	mc := &MetricsCollector{
		numCPU: runtime.NumCPU(),
	}
	mc.lastCPUTime, _ = mc.readCPUTime()
	mc.lastCheckTime = time.Now()
	return mc
}

func (mc *MetricsCollector) Collect() ProcessMetrics {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics := ProcessMetrics{}

	metrics.MemoryBytes = mc.readMemoryRSS()

	currentCPUTime, err := mc.readCPUTime()
	if err == nil {
		now := time.Now()
		elapsed := now.Sub(mc.lastCheckTime)

		if elapsed > 0 && mc.lastCPUTime > 0 {
			cpuDelta := currentCPUTime - mc.lastCPUTime
			clockTick := uint64(100)
			cpuSeconds := float64(cpuDelta) / float64(clockTick)
			elapsedSeconds := elapsed.Seconds()

			if elapsedSeconds > 0 {
				metrics.CPUPercent = (cpuSeconds / elapsedSeconds) * 100.0
			}
		}

		mc.lastCPUTime = currentCPUTime
		mc.lastCheckTime = now
	}

	return metrics
}

func (mc *MetricsCollector) readMemoryRSS() uint64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}

	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}

	rssPages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}

	pageSize := uint64(os.Getpagesize())
	return rssPages * pageSize
}

func (mc *MetricsCollector) readCPUTime() (uint64, error) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, err
	}

	content := string(data)
	closeParenIdx := strings.LastIndex(content, ")")
	if closeParenIdx == -1 {
		return 0, nil
	}

	fields := strings.Fields(content[closeParenIdx+1:])
	if len(fields) < 13 {
		return 0, nil
	}

	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, err
	}

	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, err
	}

	return utime + stime, nil
}
