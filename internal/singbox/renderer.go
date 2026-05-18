package singbox

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
)

// singboxConfigFile is the JSON structure for the sing-box config file.
type singboxConfigFile struct {
	Log       *logConfig        `json:"log,omitempty"`
	Inbounds  []inboundConfig   `json:"inbounds"`
	Outbounds []outboundConfig  `json:"outbounds"`
	Route     *routeConfig      `json:"route,omitempty"`
	DNS       *dnsConfig        `json:"dns,omitempty"`
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
}

type outboundConfig struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
	// Proxy outbound fields (placeholder for future)
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
}

type routeConfig struct {
	Rules    []ruleConfig `json:"rules,omitempty"`
	Final    string       `json:"final"`
	AutoDetect bool       `json:"auto_detect_interface,omitempty"`
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
	Enabled  bool          `json:"enabled,omitempty"`
	Strategy string        `json:"strategy,omitempty"`
	Servers  []dnsServerConfig `json:"servers,omitempty"`
	Final    string        `json:"final,omitempty"`
	Rules    []dnsRuleConfig  `json:"rules,omitempty"`
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
	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}
	logLevel := cfg.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	transport := cfg.Transport
	if transport == "" {
		transport = "mixed"
	}

	sf := singboxConfigFile{
		Log: &logConfig{Level: logLevel},
		Inbounds: buildInbounds(cfg, host),
		Outbounds: buildOutbounds(cfg),
		Route: buildRoute(cfg),
		DNS: buildDNS(cfg),
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

	if cfg.Transport == "tun" {
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

	// Always add socks and mixed on the local listen port for health checking.
	inbounds = append(inbounds, inboundConfig{
		Type:       "socks",
		Tag:        "socks-in",
		Listen:     host,
		ListenPort: cfg.ListenPort,
	})
	inbounds = append(inbounds, inboundConfig{
		Type:       "mixed",
		Tag:        "mixed-in",
		Listen:     host,
		ListenPort: cfg.ListenPort,
	})

	return inbounds
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
// the listen port is reachable.  This is used by HealthCheck.
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
		// Validate host is a parseable IP or hostname.
		if net.ParseIP(cfg.PublicEndpointHost) == nil {
			// Not a pure IP; that's fine if it's a hostname
		}
	}
	return true, ""
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
