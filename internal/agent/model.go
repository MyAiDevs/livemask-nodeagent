// Package agent implements the NodeAgent registration, heartbeat, and
// system metrics collection subsystem. TASK-NODE-001.
package agent

// RegisterRequest is sent to POST /internal/agent/register on startup.
type RegisterRequest struct {
	NodeID       string `json:"node_id"`
	AgentVersion string `json:"agent_version"`
	Timestamp    int64  `json:"timestamp"`
}

// RegisterResponse from Backend after registration.
type RegisterResponse struct {
	NodeID  string `json:"node_id"`
	Status  string `json:"status"` // pending_review / approved / active / rejected
	Message string `json:"message,omitempty"`
}

// HeartbeatRequest is sent to POST /internal/agent/heartbeat periodically.
type HeartbeatRequest struct {
	NodeID        string        `json:"node_id"`
	AgentVersion  string        `json:"agent_version"`
	ConfigVersion int           `json:"config_version"`
	ConfigHash    string        `json:"config_hash"`
	HealthStatus  string        `json:"health_status"`  // healthy / degraded / down
	Degraded      bool          `json:"degraded"`
	DegradedReason string       `json:"degraded_reason,omitempty"`
	SingboxStatus string        `json:"singbox_status"` // healthy / unhealthy / unknown
	SystemMetrics SystemMetrics `json:"system_metrics"`
	Timestamp     int64         `json:"timestamp"`
}

// SystemMetrics holds the self-collected system performance data.
type SystemMetrics struct {
	CPUPercent        float64 `json:"cpu_percent"`
	MemoryPercent     float64 `json:"memory_percent"`
	MemoryUsedMB      int64   `json:"memory_used_mb"`
	Load1             float64 `json:"load_1"`
	Load5             float64 `json:"load_5"`
	Load15            float64 `json:"load_15"`
	ActiveConnections int     `json:"active_connections"`
}

// HeartbeatResponse from Backend after a heartbeat POST.
type HeartbeatResponse struct {
	Accepted       bool   `json:"accepted"`
	ServerTime     int64  `json:"server_time,omitempty"`
	FallbackAction string `json:"fallback_action,omitempty"`
}

// AgentStatus is an observable snapshot of the agent's registration and
// heartbeat state. Exposed via /agent/status HTTP endpoint.
type AgentStatus struct {
	IsDeployed        bool    `json:"is_deployed"`
	Registered        bool    `json:"registered"`
	NodeStatus        string  `json:"node_status,omitempty"`
	LastRegisterAt    *int64  `json:"last_register_at,omitempty"`
	LastRegisterErr   string  `json:"last_register_error,omitempty"`
	HeartbeatsSent    int64   `json:"heartbeats_sent"`
	LastHeartbeatAt   *int64  `json:"last_heartbeat_at,omitempty"`
	LastHeartbeatOK   bool    `json:"last_heartbeat_ok"`
	LastHeartbeatErr  string  `json:"last_heartbeat_error,omitempty"`
	HealthStatus      string  `json:"health_status"`
	Degraded          bool    `json:"degraded"`
	DegradedReason    string  `json:"degraded_reason,omitempty"`
	SingboxStatus     string  `json:"singbox_status"`
	LastSystemMetrics *SystemMetrics `json:"last_system_metrics,omitempty"`
}

// ConfigProvider defines the interface the agent manager uses to read the
// current config version and hash from the config subsystem.
type ConfigProvider interface {
	ConfigVersion() int
	ConfigHash() string
	IsDegraded() bool
}
