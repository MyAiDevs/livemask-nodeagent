// Package agent implements the NodeAgent registration, heartbeat, and
// system metrics collection subsystem. TASK-NODE-001.
package agent

import "github.com/MyAiDevs/livemask-nodeagent/internal/singbox"

// RegisterRequest matches Backend 02794f0 internal/node/types.go RegisterRequest.
type RegisterRequest struct {
	NodeID       string `json:"node_id,omitempty"`
	NodeSecret   string `json:"node_secret,omitempty"`
	NodeName     string `json:"node_name,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	IPAddress    string `json:"ip_address,omitempty"`
}

// RegisterResponse matches Backend 02794f0 internal/node/types.go RegisterResponse.
type RegisterResponse struct {
	NodeID     string `json:"node_id"`
	NodeSecret string `json:"node_secret,omitempty"`
	Status     string `json:"status"`
}

// HeartbeatRequest matches Backend 02794f0 internal/node/types.go HeartbeatRequest.
type HeartbeatRequest struct {
	AgentVersion      string  `json:"agent_version,omitempty"`
	ConfigVersion     int     `json:"config_version"`
	ConfigHash        string  `json:"config_hash,omitempty"`
	SingboxStatus     string  `json:"singbox_status,omitempty"`
	LoadScore         int     `json:"load_score"`
	CPUUsage          float64 `json:"cpu_usage"`
	MemoryUsage       float64 `json:"memory_usage"`
	NetworkTxBytes    int64   `json:"network_tx_bytes"`
	NetworkRxBytes    int64   `json:"network_rx_bytes"`
	ActiveConnections int     `json:"active_connections"`
	Degraded          bool    `json:"degraded"`
	DegradedReason    string  `json:"degraded_reason,omitempty"`
}

// HeartbeatResponse matches Backend 02794f0 internal/node/types.go HeartbeatResponse.
type HeartbeatResponse struct {
	OK                  bool   `json:"ok"`
	ServerConfigVersion int    `json:"server_config_version"`
	Degraded            bool   `json:"degraded,omitempty"`
}

// SystemMetrics holds the self-collected system performance data.
type SystemMetrics struct {
	CPUPercent        float64
	MemoryPercent     float64
	MemoryUsedMB      int64
	Load1             float64
	Load5             float64
	Load15            float64
	ActiveConnections int
}

// Identity is the local persistence format for node registration credentials.
type Identity struct {
	NodeID     string `json:"node_id"`
	NodeSecret string `json:"node_secret"`
}

// SingboxRuntimeStatus is a type alias for singbox.RuntimeStatus.
type SingboxRuntimeStatus = singbox.RuntimeStatus

// SingboxStatusProvider is an interface the agent manager uses to read
// the current sing-box runtime status for heartbeat and observability.
type SingboxStatusProvider interface {
	Status() SingboxRuntimeStatus
}

// AgentStatus is an observable snapshot of the agent's registration and
// heartbeat state. Exposed via /agent/status HTTP endpoint.
type AgentStatus struct {
	IsDeployed        bool           `json:"is_deployed"`
	Registered        bool           `json:"registered"`
	IdentityFile      string         `json:"identity_file,omitempty"`
	NodeID            string         `json:"node_id,omitempty"`
	NodeStatus        string         `json:"node_status,omitempty"`
	LastRegisterAt    *int64         `json:"last_register_at,omitempty"`
	LastRegisterErr   string         `json:"last_register_error,omitempty"`
	HeartbeatsSent    int64          `json:"heartbeats_sent"`
	LastHeartbeatAt   *int64         `json:"last_heartbeat_at,omitempty"`
	LastHeartbeatOK   bool           `json:"last_heartbeat_ok"`
	LastHeartbeatErr  string         `json:"last_heartbeat_error,omitempty"`
	HealthStatus      string         `json:"health_status"`
	Degraded          bool           `json:"degraded"`
	DegradedReason    string         `json:"degraded_reason,omitempty"`
	SingboxStatus     string         `json:"singbox_status"`
	Singbox           *SingboxRuntimeStatus `json:"singbox,omitempty"`
	LastSystemMetrics *SystemMetrics `json:"last_system_metrics,omitempty"`
}

// ConfigProvider defines the interface the agent manager uses to read the
// current config version and hash from the config subsystem.
type ConfigProvider interface {
	ConfigVersion() int
	ConfigHash() string
	IsDegraded() bool
}
