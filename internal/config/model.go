// Package config implements the NodeAgent configuration sync, cache, and
// hot-reload subsystem. TASK-NA-CONFIG-001.
package config

import (
	"encoding/json"
	"time"
)

// ConfigResponse is the response from GET /internal/agent/config.
// See docs/contracts/api/config-center.md#3-nodeagent-read.
type ConfigResponse struct {
	SchemaVersion  string          `json:"schema_version"`
	ConfigKey      string          `json:"config_key"`
	ConfigVersion  int             `json:"config_version"`
	ConfigHash     string          `json:"config_hash"`
	Payload        json.RawMessage `json:"payload"`
	FallbackAction string          `json:"fallback_action"`
	PublishedAt    time.Time       `json:"published_at"`
}

// RuntimeConfig is the parsed payload for nodeagent.runtime_config.
// See docs/contracts/config/core-configs.md#nodeagentruntime_config.
type RuntimeConfig struct {
	SchemaVersion string           `json:"schema_version"`
	Reporting     ReportingConfig  `json:"reporting"`
	DegradedMode  DegradedConfig   `json:"degraded_mode"`
	Singbox       SingboxConfig    `json:"singbox"`
}

// ReportingConfig holds intervals and buffering limits.
type ReportingConfig struct {
	HeartbeatIntervalSeconds  int `json:"heartbeat_interval_seconds"`
	BatchUploadIntervalSeconds int `json:"batch_upload_interval_seconds"`
	MaxOfflineBufferItems     int `json:"max_offline_buffer_items"`
}

// DegradedConfig controls degraded mode behaviour.
type DegradedConfig struct {
	Enabled    bool `json:"enabled"`
	AutoRecover bool `json:"auto_recover"`
}

// SingboxConfig holds sing-box health check and runtime settings.
type SingboxConfig struct {
	HealthCheckTimeoutSeconds int    `json:"health_check_timeout_seconds"`
	ListenHost                string `json:"listen_host,omitempty"`
	ListenPort                int    `json:"listen_port,omitempty"`
	LogLevel                  string `json:"log_level,omitempty"`
	Transport                 string `json:"transport,omitempty"`
	ProtocolProfile           string `json:"protocol_profile,omitempty"`
	PublicEndpointHost        string `json:"public_endpoint_host,omitempty"`
	PublicEndpointPort        int    `json:"public_endpoint_port,omitempty"`
	TunInterfaceName          string `json:"tun_interface_name,omitempty"`
	TunMTU                    int    `json:"tun_mtu,omitempty"`
	DNSEnabled                bool   `json:"dns_enabled"`
	DNSStrategy               string `json:"dns_strategy,omitempty"`
	DNSServers                string `json:"dns_servers,omitempty"` // comma-separated, e.g. "1.1.1.1,8.8.8.8"
	RouteGlobal               bool   `json:"route_global"`
	BypassLAN                 bool   `json:"bypass_lan"`
	ProxyOutboundTag          string `json:"proxy_outbound_tag,omitempty"`
}

// DefaultRuntimeConfig returns a safe default configuration.
func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		SchemaVersion: "1.0",
		Reporting: ReportingConfig{
			HeartbeatIntervalSeconds:  60,
			BatchUploadIntervalSeconds: 300,
			MaxOfflineBufferItems:     10000,
		},
		DegradedMode: DegradedConfig{
			Enabled:     true,
			AutoRecover: false,
		},
		Singbox: SingboxConfig{
			HealthCheckTimeoutSeconds: 5,
			ListenHost:                "127.0.0.1",
			ListenPort:                10808,
			LogLevel:                  "info",
			Transport:                 "mixed",
			ProtocolProfile:           "tcp_udp",
			DNSEnabled:                true,
			DNSStrategy:               "prefer_ipv4",
			DNSServers:                "1.1.1.1,8.8.8.8",
			RouteGlobal:               false,
			BypassLAN:                 true,
		},
	}
}

// Clone returns a deep copy of the RuntimeConfig.
func (c *RuntimeConfig) Clone() RuntimeConfig {
	return *c
}

// CacheEntry is the on-disk format for the last-known-good config.
type CacheEntry struct {
	Response   *ConfigResponse `json:"response,omitempty"`
	Parsed     *RuntimeConfig  `json:"parsed,omitempty"`
	FetchedAt  time.Time       `json:"fetched_at"`
}

// ConfigStatus is an observable snapshot of the current config state.
type ConfigStatus struct {
	ConfigVersion int       `json:"config_version"`
	ConfigHash    string    `json:"config_hash"`
	ConfigKey     string    `json:"config_key"`
	SchemaVersion string    `json:"schema_version"`
	IsDegraded    bool      `json:"is_degraded"`
	LastFetchAt   *time.Time `json:"last_fetch_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}
