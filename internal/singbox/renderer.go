package singbox

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	sbprotocol "github.com/MyAiDevs/livemask-nodeagent/internal/singbox/protocol"
)

// singboxConfigFile is the JSON structure for the sing-box config file.
type singboxConfigFile struct {
	Log       *logConfig       `json:"log,omitempty"`
	Inbounds  []map[string]any `json:"inbounds"`
	Outbounds []map[string]any `json:"outbounds"`
	Route     map[string]any   `json:"route,omitempty"`
	DNS       map[string]any   `json:"dns,omitempty"`
}

type logConfig struct {
	Level string `json:"level,omitempty"`
}

type inboundConfig struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen,omitempty"`
	ListenPort int    `json:"listen_port,omitempty"`
	// Tun-specific
	InterfaceName string `json:"interface_name,omitempty"`
	MTU           int    `json:"mtu,omitempty"`
	// TLS metadata (TASK-NODEAGENT-SINGBOX-003)
	TLS *inboundTLSConfig `json:"tls,omitempty"`
}

type inboundTLSConfig struct {
	Enabled    bool     `json:"enabled,omitempty"`
	ServerName string   `json:"server_name,omitempty"`
	ALPN       []string `json:"alpn,omitempty"`
}

type outboundConfig struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
	// Proxy outbound fields (placeholder for future)
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
}

type routeConfig struct {
	Rules      []ruleConfig `json:"rules,omitempty"`
	Final      string       `json:"final"`
	AutoDetect bool         `json:"auto_detect_interface,omitempty"`
}

type ruleConfig struct {
	Outbound string   `json:"outbound"`
	Network  string   `json:"network,omitempty"` // tcp, udp
	Port     []int    `json:"port,omitempty"`
	GeoIP    []string `json:"geoip,omitempty"`
	Geosite  []string `json:"geosite,omitempty"`
	IPCIDR   []string `json:"ip_cidr,omitempty"`
}

type dnsConfig struct {
	Enabled  bool              `json:"enabled,omitempty"`
	Strategy string            `json:"strategy,omitempty"`
	Servers  []dnsServerConfig `json:"servers,omitempty"`
	Final    string            `json:"final,omitempty"`
	Rules    []dnsRuleConfig   `json:"rules,omitempty"`
}

type dnsServerConfig struct {
	Tag      string `json:"tag"`
	Address  string `json:"address"`
	Strategy string `json:"strategy,omitempty"`
}

type dnsRuleConfig struct {
	Outbound string   `json:"outbound"`
	Server   string   `json:"server"`
	Geosite  []string `json:"geosite,omitempty"`
}

// Render generates a sing-box config file and writes it atomically to ConfigPath.
func Render(cfg *SingboxConfig) error {
	if cfg == nil {
		return fmt.Errorf("singbox config is nil")
	}
	if cfg.ListenPort <= 0 || cfg.ListenPort > 65535 {
		return fmt.Errorf("singbox listen port %d is invalid (must be 1-65535)", cfg.ListenPort)
	}
	if cfg.PublicEndpointPort != 0 && (cfg.PublicEndpointPort <= 0 || cfg.PublicEndpointPort > 65535) {
		return fmt.Errorf("singbox public endpoint port %d is invalid (must be 1-65535)", cfg.PublicEndpointPort)
	}
	if cfg.PublicProbePort != 0 && (cfg.PublicProbePort <= 0 || cfg.PublicProbePort > 65535) {
		return fmt.Errorf("singbox public probe port %d is invalid (must be 1-65535)", cfg.PublicProbePort)
	}
	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}
	logLevel := cfg.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	protocolCfg := ToProtocolConfigWithHost(cfg, host)
	profileName := sbprotocol.ResolveProfileName(protocolCfg)
	profile, ok := sbprotocol.Get(profileName)
	if !ok {
		return fmt.Errorf("singbox protocol profile %q is not registered", sanitizeError(profileName))
	}
	protocolCfg.Profile = profileName
	if err := profile.Validate(protocolCfg); err != nil {
		return fmt.Errorf("validate singbox protocol profile %q: %s", sanitizeError(profileName), sanitizeError(err.Error()))
	}
	rendered, err := profile.Render(protocolCfg)
	if err != nil {
		return fmt.Errorf("render singbox protocol profile %q: %s", sanitizeError(profileName), sanitizeError(err.Error()))
	}
	if rendered == nil {
		return fmt.Errorf("render singbox protocol profile %q returned nil result", profileName)
	}

	sf := singboxConfigFile{
		Log:       &logConfig{Level: logLevel},
		Inbounds:  rendered.Inbounds,
		Outbounds: firstNonEmptyMaps(rendered.Outbounds, buildOutbounds(cfg)),
		Route:     firstNonNilMap(rendered.Route, buildRoute(cfg)),
		DNS:       firstNonNilMap(rendered.DNS, buildDNS(cfg)),
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal singbox config: %w", err)
	}

	// Atomic write: temp file + rename.
	tmpPath := cfg.ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write temp singbox config: %w", err)
	}
	if err := os.Rename(tmpPath, cfg.ConfigPath); err != nil {
		return fmt.Errorf("rename singbox config: %w", err)
	}
	return nil
}

func ToProtocolConfig(cfg *SingboxConfig) sbprotocol.ProtocolConfig {
	host := ""
	if cfg != nil {
		host = cfg.ListenHost
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return ToProtocolConfigWithHost(cfg, host)
}

func ToProtocolConfigWithHost(cfg *SingboxConfig, host string) sbprotocol.ProtocolConfig {
	if cfg == nil {
		return sbprotocol.ProtocolConfig{}
	}
	return sbprotocol.ProtocolConfig{
		Profile:            cfg.ProtocolProfile,
		Transport:          cfg.Transport,
		ListenHost:         host,
		ListenPort:         cfg.ListenPort,
		PublicEndpointHost: cfg.PublicEndpointHost,
		PublicEndpointPort: cfg.PublicEndpointPort,
		SNI:                cfg.SNI,
		ALPN:               cfg.ALPN,
		TLS: sbprotocol.TLSConfig{
			Enabled: cfg.TLSEnabled,
			SNI:     cfg.SNI,
			ALPN:    cfg.ALPN,
		},
		DNS: sbprotocol.DNSConfig{
			Enabled:  cfg.DNSEnabled,
			Strategy: cfg.DNSStrategy,
			Servers:  append([]string(nil), cfg.DNSServers...),
		},
		Route: sbprotocol.RouteConfig{
			Global:           cfg.RouteGlobal,
			BypassLAN:        cfg.BypassLAN,
			FinalOutbound:    cfg.ProxyOutboundTag,
			ProxyOutboundTag: cfg.ProxyOutboundTag,
		},
		Raw: map[string]any{
			"tun_interface_name":       cfg.TunInterfaceName,
			"tun_mtu":                  cfg.TunMTU,
			"hysteria2_up_mbps":        cfg.Hysteria2UpMbps,
			"hysteria2_down_mbps":      cfg.Hysteria2DownMbps,
			"hysteria2_obfs_type":      cfg.Hysteria2ObfsType,
			"hysteria2_auth_env":       cfg.Hysteria2AuthEnv,
			"hysteria2_obfs_password_env": cfg.Hysteria2ObfsPasswordEnv,
		},
	}
}

func profileForConfig(cfg *SingboxConfig) (sbprotocol.ProtocolProfile, sbprotocol.ProtocolConfig, error) {
	if cfg == nil {
		return nil, sbprotocol.ProtocolConfig{}, fmt.Errorf("singbox config is nil")
	}
	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}
	protocolCfg := ToProtocolConfigWithHost(cfg, host)
	profileName := sbprotocol.ResolveProfileName(protocolCfg)
	profile, ok := sbprotocol.Get(profileName)
	if !ok {
		return nil, sbprotocol.ProtocolConfig{}, fmt.Errorf("singbox protocol profile %q is not registered", sanitizeError(profileName))
	}
	protocolCfg.Profile = profileName
	return profile, protocolCfg, nil
}

func ProfileEndpointMetadata(cfg *SingboxConfig) (sbprotocol.EndpointMetadata, error) {
	profile, protocolCfg, err := profileForConfig(cfg)
	if err != nil {
		return sbprotocol.EndpointMetadata{}, err
	}
	return profile.Endpoint(protocolCfg), nil
}

func ProfileHealthChecks(cfg *SingboxConfig) ([]sbprotocol.HealthCheckSpec, error) {
	profile, protocolCfg, err := profileForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return profile.HealthChecks(protocolCfg), nil
}

func firstNonEmptyMaps(primary, fallback []map[string]any) []map[string]any {
	if len(primary) > 0 {
		return primary
	}
	return fallback
}

func firstNonNilMap(primary, fallback map[string]any) map[string]any {
	if primary != nil {
		return primary
	}
	return fallback
}

func buildOutbounds(cfg *SingboxConfig) []map[string]any {
	outbounds := []map[string]any{
		{"type": "direct", "tag": "direct"},
		{"type": "block", "tag": "block"},
	}

	// Add proxy placeholder outbound if a tag is specified.
	if cfg.ProxyOutboundTag != "" {
		outbounds = append(outbounds, map[string]any{
			"type": "direct", // placeholder - will be replaced with real proxy
			"tag":  cfg.ProxyOutboundTag,
		})
	}

	return outbounds
}

func buildRoute(cfg *SingboxConfig) map[string]any {
	r := map[string]any{"final": "direct", "auto_detect_interface": true}

	if cfg.RouteGlobal && cfg.ProxyOutboundTag != "" {
		r["final"] = cfg.ProxyOutboundTag
	}

	var rules []map[string]any

	if cfg.BypassLAN {
		// Bypass private/LAN IP ranges.
		rules = append(rules, map[string]any{
			"outbound": "direct",
			"ip_cidr":  privateCIDRs(),
		})
	}

	// DNS query rule.
	rules = append(rules, map[string]any{
		"outbound": "dns-out",
		"network":  "udp",
		"port":     []int{53},
	})

	// If not global, default outbound is still direct for non-proxied traffic.
	if len(rules) > 0 {
		r["rules"] = rules
	}
	return r
}

func buildDNS(cfg *SingboxConfig) map[string]any {
	if !cfg.DNSEnabled {
		return nil
	}

	d := map[string]any{
		"enabled":  true,
		"strategy": cfg.DNSStrategy,
		"final":    "dns-default",
	}

	var servers []map[string]any
	if len(cfg.DNSServers) > 0 {
		for i, addr := range cfg.DNSServers {
			tag := fmt.Sprintf("dns-srv-%d", i+1)
			servers = append(servers, map[string]any{
				"tag":     tag,
				"address": addr,
			})
		}
	}

	// Default server if none specified.
	if len(servers) == 0 {
		servers = append(servers, map[string]any{
			"tag":     "dns-default",
			"address": "https://1.1.1.1/dns-query",
		})
	}
	d["servers"] = servers

	return d
}

// IsEndpointReady checks whether the public endpoint fields are valid and
// (optionally) the public host:port is reachable.  This is used by HealthCheck.
// TASK-NODEAGENT-SINGBOX-003: considers health_check_mode.
func IsEndpointReady(cfg *SingboxConfig) (bool, string) {
	if cfg == nil || !cfg.Enabled {
		return false, "singbox not enabled"
	}
	if cfg.ListenPort <= 0 || cfg.ListenPort > 65535 {
		return false, "listen port invalid"
	}
	if cfg.PublicEndpointPort != 0 {
		if cfg.PublicEndpointPort <= 0 || cfg.PublicEndpointPort > 65535 {
			return false, "public endpoint port invalid"
		}
		if cfg.PublicEndpointHost == "" {
			return false, "public endpoint host is empty but port is set"
		}
	}
	if cfg.PublicProbePort != 0 && (cfg.PublicProbePort <= 0 || cfg.PublicProbePort > 65535) {
		return false, "public probe port invalid"
	}
	return true, ""
}

// PublicProbeHealthCheck performs a TCP dial to the configured public probe target.
// Returns ok=true, "" if the dial succeeds.
// If public probe is disabled, returns true with no error.
// TASK-NODEAGENT-SINGBOX-003.
func PublicProbeHealthCheck(cfg *SingboxConfig) (ok bool, reason string) {
	if cfg == nil || !cfg.Enabled {
		return false, "singbox not enabled"
	}
	if !EffectivePublicProbeEnabled(cfg) {
		return true, ""
	}
	host := cfg.PublicProbeHost
	if host == "" {
		host = cfg.PublicEndpointHost
	}
	port := cfg.PublicProbePort
	if port <= 0 {
		port = cfg.PublicEndpointPort
	}
	if host == "" || port <= 0 {
		return false, "public probe target not configured (no host/port)"
	}

	timeoutMs := cfg.PublicProbeTimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		return false, sanitizeError(fmt.Sprintf("public probe dial failed: %v", err))
	}
	conn.Close()
	return true, ""
}

func EffectivePublicProbeEnabled(cfg *SingboxConfig) bool {
	if cfg == nil {
		return false
	}
	return cfg.PublicProbeEnabled ||
		cfg.HealthCheckMode == string(HealthCheckPublic) ||
		cfg.HealthCheckMode == string(HealthCheckBoth)
}

func sanitizeError(message string) string {
	return sbprotocol.RedactString(message)
}

func privateCIDRs() []string {
	return []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"fc00::/7",
		"fe80::/10",
	}
}

// ProxyHostPort returns the formatted endpoint for proxy protocol health checks.
func ProxyHostPort(cfg *SingboxConfig) string {
	if cfg == nil {
		return ""
	}
	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(cfg.ListenPort))
}
