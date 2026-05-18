// Package singbox implements the NodeAgent sing-box runtime lifecycle:
// config rendering, process management, and health checking.
// TASK-NODEAGENT-SINGBOX-001, TASK-NODEAGENT-SINGBOX-002.
package singbox

// Status represents the current sing-box runtime status.
type Status string

const (
	StatusDisabled  Status = "disabled"  // SINGBOX_ENABLED=false
	StatusStopped   Status = "stopped"   // Process not running (expected)
	StatusStarting  Status = "starting"  // Process launching
	StatusRunning   Status = "running"   // Process alive + port open
	StatusUnhealthy Status = "unhealthy" // Process alive but port unreachable
	StatusFailed    Status = "failed"    // Process exited unexpectedly or failed to start
)

// RuntimeStatus is the full observable state of the sing-box runtime.
type RuntimeStatus struct {
	Enabled            bool   `json:"enabled"`
	Status             string `json:"status"`
	PID                int    `json:"pid"`
	ConfigPath         string `json:"config_path"`
	ListenHost         string `json:"listen_host"`
	ListenPort         int    `json:"listen_port"`
	Transport          string `json:"transport,omitempty"`        // socks / mixed / tun
	ProtocolProfile    string `json:"protocol_profile,omitempty"` // mixed / socks / tun / reserved profiles
	PublicEndpointHost string `json:"public_endpoint_host,omitempty"`
	PublicEndpointPort int    `json:"public_endpoint_port,omitempty"`
	EndpointReady      bool   `json:"endpoint_ready"`
	// Probe status (TASK-NODEAGENT-SINGBOX-003).
	PublicProbeEnabled bool   `json:"public_probe_enabled"`
	PublicProbeOK      bool   `json:"public_probe_ok"`
	PublicProbeLastErr string `json:"public_probe_last_error,omitempty"`
	PublicProbeLastAt  *int64 `json:"public_probe_last_at,omitempty"`
	LastStartedAt      *int64 `json:"last_started_at,omitempty"`
	LastStoppedAt      *int64 `json:"last_stopped_at,omitempty"`
	LastHealthCheckAt  *int64 `json:"last_health_check_at,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	RestartCount       int    `json:"restart_count"`
}

// HealthCheckMode defines how endpoint readiness is verified.
type HealthCheckMode string

const (
	HealthCheckLocal  HealthCheckMode = "local"  // local port only
	HealthCheckPublic HealthCheckMode = "public" // public TCP dial
	HealthCheckBoth   HealthCheckMode = "both"   // local port + public dial
)

// SingboxConfig is the generation input for the sing-box config file.
// It is populated from the config center payload + env overrides.
type SingboxConfig struct {
	Enabled               bool
	BinPath               string
	ConfigPath            string
	WorkDir               string
	LogPath               string
	ListenHost            string
	ListenPort            int
	LogLevel              string
	Transport             string // socks, mixed, tun
	ProtocolProfile       string // mixed, socks, tun, or reserved future profile
	PublicEndpointHost    string // public-facing host (may differ from listen)
	PublicEndpointPort    int    // public-facing port (may differ from listen)
	TunInterfaceName      string // tun device name (if transport=tun)
	TunMTU                int    // tun MTU (if transport=tun)
	DNSEnabled            bool
	DNSStrategy           string // prefer_ipv4, prefer_ipv6, ipv4_only, ipv6_only
	DNSServers            []string
	RouteGlobal           bool   // global proxy (no bypass)
	BypassLAN             bool   // bypass local/private ranges
	ProxyOutboundTag      string // outbound tag for proxy (e.g. "proxy")
	RestartOnConfigChange bool
	// TLS / public endpoint fields (TASK-NODEAGENT-SINGBOX-003).
	TLSEnabled bool   `json:"tls_enabled,omitempty"`
	SNI        string `json:"sni,omitempty"`
	ALPN       string `json:"alpn,omitempty"`
	// Health probe configuration.
	PublicProbeEnabled   bool   `json:"public_probe_enabled,omitempty"`
	PublicProbeHost      string `json:"public_probe_host,omitempty"`
	PublicProbePort      int    `json:"public_probe_port,omitempty"`
	PublicProbeTimeoutMs int    `json:"public_probe_timeout_ms,omitempty"`
	HealthCheckMode      string `json:"health_check_mode,omitempty"` // local / public / both
}
