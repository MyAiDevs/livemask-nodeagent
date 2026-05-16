package agent

// MetricsCollector defines the interface for collecting system metrics.
// Implementations can use /proc on Linux or return mock data for testing.
type MetricsCollector interface {
	Collect() (*SystemMetrics, error)
}

// NewSystemCollector creates the appropriate system collector for the current
// platform. On Linux it uses /proc, on other platforms it returns a stub.
func NewSystemCollector() MetricsCollector {
	return newSystemCollector()
}
