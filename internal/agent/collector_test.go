package agent

import (
	"encoding/json"
	"testing"
)

// TestNewSystemCollector verifies the collector is created without panic.
func TestNewSystemCollector(t *testing.T) {
	c := NewSystemCollector()
	if c == nil {
		t.Fatal("NewSystemCollector returned nil")
	}
}

// TestCollectorReturnsData verifies the collector returns non-nil metrics
// (even if partial). On macOS, /proc won't exist so the non-linux stub will
// be used.
func TestCollectorReturnsData(t *testing.T) {
	c := NewSystemCollector()
	metrics, err := c.Collect()
	if err != nil {
		t.Logf("collector returned error (acceptable): %v", err)
	}
	if metrics == nil {
		t.Fatal("collector returned nil metrics")
	}
	t.Logf("CPU: %.1f%%, Memory: %.1f%% (%d MB), Load: %.2f / %.2f / %.2f",
		metrics.CPUPercent, metrics.MemoryPercent, metrics.MemoryUsedMB,
		metrics.Load1, metrics.Load5, metrics.Load15)
}

// TestMockCollector verifies the mock works correctly.
func TestMockCollector(t *testing.T) {
	m := &mockCollector{}
	metrics, err := m.Collect()
	if err != nil {
		t.Fatalf("mock collector error: %v", err)
	}
	if metrics.CPUPercent != 15.0 {
		t.Fatalf("expected CPU 15%%, got %.1f%%", metrics.CPUPercent)
	}
	if metrics.MemoryUsedMB != 1024 {
		t.Fatalf("expected memory 1024 MB, got %d", metrics.MemoryUsedMB)
	}
}

// TestSystemMetricsJSON verifies the JSON serialisation of system metrics.
func TestSystemMetricsJSON(t *testing.T) {
	m := &SystemMetrics{
		CPUPercent:    25.5,
		MemoryPercent: 60.0,
		MemoryUsedMB:  3072,
		Load1:         1.5,
		Load5:         1.0,
		Load15:        0.8,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("Metrics JSON: %s", string(data))
	if len(data) == 0 {
		t.Fatal("empty marshal result")
	}
}
