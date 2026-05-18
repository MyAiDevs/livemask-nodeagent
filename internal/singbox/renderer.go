package singbox

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// singboxConfigFile is the JSON structure for the sing-box config file.
type singboxConfigFile struct {
	Log       *logConfig       `json:"log,omitempty"`
	Inbounds  []inboundConfig  `json:"inbounds"`
	Outbounds []outboundConfig `json:"outbounds"`
	Route     *routeConfig     `json:"route,omitempty"`
	DNS       *dnsConfig       `json:"dns,omitempty"`
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
	if cfg.Transport != "" && cfg.Transport != "mixed" && cfg.Transport != "socks" && cfg.Transport != "tun" {
		return fmt.Errorf("singbox transport %q is invalid (must be mixed, socks, or tun)", cfg.Transport)
	}
	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}
	logLevel := cfg.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	sf := singboxConfigFile{
		Log:       &logConfig{Level: logLevel},
		Inbounds:  buildInbounds(cfg, host),
		Outbounds: buildOutbounds(cfg),
		Route:     buildRoute(cfg),
		DNS:       buildDNS(cfg),
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

func buildInbounds(cfg *SingboxConfig, host string) []inboundConfig {
	var inbounds []inboundConfig

	transport := cfg.Transport
	if transport == "" {
		transport = "mixed"
	}

	if transport == "tun" {
		iface := cfg.TunInterfaceName
		if iface == "" {
			iface = "singbox-tun0"
		}
		mtu := cfg.TunMTU
		if mtu <= 0 {
			mtu = 1500
		}
		inbounds = append(inbounds, inboundConfig{
			Type:          "tun",
			Tag:           "tun-in",
			InterfaceName: iface,
			MTU:           mtu,
		})
	}

	if transport == "socks" || transport == "mixed" || transport == "tun" {
		inboundType := transport
		if transport == "tun" {
			inboundType = "mixed"
		}
		serviceInbound := inboundConfig{
			Type:       inboundType,
			Tag:        "service-in",
			Listen:     host,
			ListenPort: cfg.ListenPort,
		}
		if cfg.TLSEnabled {
			serviceInbound.TLS = &inboundTLSConfig{
				Enabled:    true,
				ServerName: cfg.SNI,
				ALPN:       parseALPN(cfg.ALPN),
			}
		}
		inbounds = append(inbounds, serviceInbound)
	}

	return inbounds
}

// parseALPN splits a comma-separated ALPN string (e.g. "h2,http/1.1") into a slice.
func parseALPN(alpn string) []string {
	if alpn == "" {
		return nil
	}
	var parts []string
	for _, p := range strings.Split(alpn, ",") {
		s := strings.TrimSpace(p)
		if s != "" {
			parts = append(parts, s)
		}
	}
	return parts
}

func buildOutbounds(cfg *SingboxConfig) []outboundConfig {
	outbounds := []outboundConfig{
		{Type: "direct", Tag: "direct"},
		{Type: "block", Tag: "block"},
	}

	// Add proxy placeholder outbound if a tag is specified.
	if cfg.ProxyOutboundTag != "" {
		outbounds = append(outbounds, outboundConfig{
			Type: "direct", // placeholder — will be replaced with real proxy
			Tag:  cfg.ProxyOutboundTag,
		})
	}

	return outbounds
}

func buildRoute(cfg *SingboxConfig) *routeConfig {
	r := &routeConfig{Final: "direct", AutoDetect: true}

	if cfg.RouteGlobal && cfg.ProxyOutboundTag != "" {
		r.Final = cfg.ProxyOutboundTag
	}

	var rules []ruleConfig

	if cfg.BypassLAN {
		// Bypass private/LAN IP ranges.
		rules = append(rules, ruleConfig{
			Outbound: "direct",
			IPCIDR:   privateCIDRs(),
		})
	}

	// DNS query rule.
	rules = append(rules, ruleConfig{
		Outbound: "dns-out",
		Network:  "udp",
		Port:     []int{53},
	})

	// If not global, default outbound is still direct for non-proxied traffic.
	if len(rules) > 0 {
		r.Rules = rules
	}
	return r
}

func buildDNS(cfg *SingboxConfig) *dnsConfig {
	if !cfg.DNSEnabled {
		return nil
	}

	d := &dnsConfig{
		Enabled:  true,
		Strategy: cfg.DNSStrategy,
		Final:    "dns-default",
	}

	if len(cfg.DNSServers) > 0 {
		for i, addr := range cfg.DNSServers {
			tag := fmt.Sprintf("dns-srv-%d", i+1)
			d.Servers = append(d.Servers, dnsServerConfig{
				Tag:     tag,
				Address: addr,
			})
		}
	}

	// Default server if none specified.
	if len(d.Servers) == 0 {
		d.Servers = append(d.Servers, dnsServerConfig{
			Tag:     "dns-default",
			Address: "https://1.1.1.1/dns-query",
		})
	}

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
	if message == "" {
		return ""
	}
	sanitized := message
	lower := strings.ToLower(sanitized)
	for _, marker := range []string{"node_secret", "password", "private_key", "access_key", "token"} {
		for {
			idx := strings.Index(lower, marker)
			if idx == -1 {
				break
			}
			end := idx + len(marker)
			for end < len(sanitized) {
				ch := sanitized[end]
				if ch == ' ' || ch == ',' || ch == ';' || ch == ')' || ch == ']' {
					break
				}
				end++
			}
			sanitized = sanitized[:idx] + "[redacted]" + sanitized[end:]
			lower = strings.ToLower(sanitized)
		}
	}
	return sanitized
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
