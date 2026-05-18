// Package singbox implements the NodeAgent sing-box runtime lifecycle:
// config rendering, process management, and health checking.
// TASK-NODEAGENT-SINGBOX-001.
package singbox

// Status represents the current sing-box runtime status.
type Status string

const (
	StatusDisabled  Status = "disabled"   // SINGBOX_ENABLED=false
	StatusStopped   Status = "stopped"    // Process not running (expected)
	StatusStarting  Status = "starting"   // Process launching
	StatusRunning   Status = "running"    // Process alive + port open
	StatusUnhealthy Status = "unhealthy"  // Process alive but port unreachable
	StatusFailed    Status = "failed"     // Process exited unexpectedly or failed to start
)

// RuntimeStatus is the full observable state of the sing-box runtime.
type RuntimeStatus struct {
	Enabled         bool   `json:"enabled"`
	Status          string `json:"status"`
	PID             int    `json:"pid"`
	ConfigPath      string `json:"config_path"`
	ListenHost      string `json:"listen_host"`
	ListenPort      int    `json:"listen_port"`
	LastStartedAt   *int64 `json:"last_started_at,omitempty"`
	LastStoppedAt   *int64 `json:"last_stopped_at,omitempty"`
	LastHealthCheckAt *int64 `json:"last_health_check_at,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	RestartCount    int    `json:"restart_count"`
}

// SingboxConfig is the generation input for the sing-box config file.
// It is populated from the config center payload + env overrides.
type SingboxConfig struct {
	Enabled    bool
	BinPath    string
	ConfigPath string
	WorkDir    string
	LogPath    string
	ListenHost string
	ListenPort int
	LogLevel   string
	RestartOnConfigChange bool
}
