//go:build linux

package agent

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type sysCollector struct {
	numCPU     int
	prevCPUTime cpuTimes
}

type cpuTimes struct {
	user   uint64
	nice   uint64
	system uint64
	idle   uint64
	iowait uint64
	irq    uint64
	softirq uint64
	steal  uint64
	total  uint64
}

func newSystemCollector() MetricsCollector {
	return &sysCollector{numCPU: runtime.NumCPU()}
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

	memPercent, memUsedMB, memErr := readMemory()
	if memErr != nil {
		errs = append(errs, fmt.Errorf("memory: %w", memErr))
	} else {
		metrics.MemoryPercent = memPercent
		metrics.MemoryUsedMB = memUsedMB
	}

	load1, load5, load15, loadErr := readLoad()
	if loadErr != nil {
		errs = append(errs, fmt.Errorf("load: %w", loadErr))
	} else {
		metrics.Load1 = load1
		metrics.Load5 = load5
		metrics.Load15 = load15
	}

	if len(errs) > 0 {
		log.Printf("[agent] collector partial errors: %v", errs)
		return &metrics, fmt.Errorf("partial collection: %v", errs)
	}

	return &metrics, nil
}

func (sc *sysCollector) readCPUPercent() (float64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, fmt.Errorf("read /proc/stat: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 0, fmt.Errorf("empty /proc/stat")
	}

	fields := strings.Fields(lines[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, fmt.Errorf("unexpected /proc/stat format")
	}

	curr := cpuTimes{}
	curr.user, _ = strconv.ParseUint(fields[1], 10, 64)
	curr.nice, _ = strconv.ParseUint(fields[2], 10, 64)
	curr.system, _ = strconv.ParseUint(fields[3], 10, 64)
	curr.idle, _ = strconv.ParseUint(fields[4], 10, 64)
	if len(fields) > 5 {
		curr.iowait, _ = strconv.ParseUint(fields[5], 10, 64)
	}
	if len(fields) > 6 {
		curr.irq, _ = strconv.ParseUint(fields[6], 10, 64)
	}
	if len(fields) > 7 {
		curr.softirq, _ = strconv.ParseUint(fields[7], 10, 64)
	}
	if len(fields) > 8 {
		curr.steal, _ = strconv.ParseUint(fields[8], 10, 64)
	}
	curr.total = curr.user + curr.nice + curr.system + curr.idle +
		curr.iowait + curr.irq + curr.softirq + curr.steal

	if sc.prevCPUTime.total == 0 {
		sc.prevCPUTime = curr
		time.Sleep(100 * time.Millisecond)
		return sc.readCPUPercent()
	}

	totalDelta := float64(curr.total - sc.prevCPUTime.total)
	if totalDelta == 0 {
		return 0, nil
	}
	idleDelta := float64(curr.idle - sc.prevCPUTime.idle)
	sc.prevCPUTime = curr

	percent := (1.0 - idleDelta/totalDelta) * 100.0
	if percent < 0 {
		percent = 0
	}
	return percent, nil
}

func readMemory() (percent float64, usedMB int64, err error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}

	fields := parseProcMeminfo(string(data))
	totalKB := fields["MemTotal"]
	availKB := fields["MemAvailable"]
	if totalKB == 0 {
		return 0, 0, fmt.Errorf("MemTotal not found")
	}
	if availKB == 0 {
		return 0, 0, fmt.Errorf("MemAvailable not found")
	}

	usedKB := totalKB - availKB
	usedMB = int64(usedKB / 1024)
	percent = float64(usedKB) / float64(totalKB) * 100.0
	return
}

func parseProcMeminfo(data string) map[string]uint64 {
	result := make(map[string]uint64)
	for _, line := range strings.Split(data, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			val, _ := strconv.ParseUint(parts[1], 10, 64)
			result[key] = val
		}
	}
	return result
}

func readLoad() (load1, load5, load15 float64, err error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read /proc/loadavg: %w", err)
	}

	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("unexpected /proc/loadavg format")
	}

	load1, _ = strconv.ParseFloat(fields[0], 64)
	load5, _ = strconv.ParseFloat(fields[1], 64)
	load15, _ = strconv.ParseFloat(fields[2], 64)
	return
}
