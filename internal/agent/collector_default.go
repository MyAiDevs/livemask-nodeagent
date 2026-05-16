//go:build !linux

package agent

import (
	"fmt"
	"log"
	"runtime"
	"time"
)

type sysCollector struct {
	lastCPU int64
}

func newSystemCollector() MetricsCollector {
	return &sysCollector{}
}

func (sc *sysCollector) Collect() (*SystemMetrics, error) {
	var metrics SystemMetrics
	var errs []error

	cpuPercent, cpuErr := sc.readCPUPercent()
	if cpuErr != nil {
		errs = append(errs, fmt.Errorf("cpu: %w", cpuErr))
	} else {
		metrics.CPUPercent = cpuPercent
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	metrics.MemoryUsedMB = int64(memStats.Alloc / 1024 / 1024)
	// Estimate total memory on non-Linux as 2GB * NumCPU (heuristic).
	totalMemMB := int64(runtime.NumCPU()) * 2048
	if totalMemMB > 0 {
		metrics.MemoryPercent = float64(metrics.MemoryUsedMB) / float64(totalMemMB) * 100.0
	}

	if len(errs) > 0 {
		log.Printf("[agent] collector partial errors (non-linux): %v", errs)
		return &metrics, fmt.Errorf("partial collection: %v", errs)
	}
	return &metrics, nil
}

func (sc *sysCollector) readCPUPercent() (float64, error) {
	now := time.Now().UnixNano()
	if sc.lastCPU == 0 {
		sc.lastCPU = now
		time.Sleep(100 * time.Millisecond)
		return sc.readCPUPercent()
	}
	sc.lastCPU = now
	// Rough heuristic based on goroutine count.
	estimated := float64(runtime.NumGoroutine()) * 0.5
	if estimated > 100 {
		estimated = 100
	}
	return estimated, nil
}
