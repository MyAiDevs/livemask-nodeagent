package protocol

import (
	"fmt"
	"strings"
)

func init() {
	for _, profile := range []ProtocolProfile{
		builtinProfile{name: "mixed"},
		builtinProfile{name: "socks"},
		builtinProfile{name: "tun"},
	} {
		if err := Register(profile); err != nil {
			panic(err)
		}
	}
}

type builtinProfile struct {
	name string
}

func (p builtinProfile) Name() string { return p.name }

func (p builtinProfile) Validate(cfg ProtocolConfig) error {
	if cfg.ListenPort <= 0 || cfg.ListenPort > 65535 {
		return fmt.Errorf("protocol profile %q listen port is invalid", p.name)
	}
	return nil
}

func (p builtinProfile) Render(cfg ProtocolConfig) (*RenderResult, error) {
	if err := p.Validate(cfg); err != nil {
		return nil, err
	}

	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}

	inbounds := make([]map[string]any, 0, 2)
	if p.name == "tun" {
		iface, _ := cfg.Raw["tun_interface_name"].(string)
		if iface == "" {
			iface = "singbox-tun0"
		}
		mtu, _ := cfg.Raw["tun_mtu"].(int)
		if mtu <= 0 {
			mtu = 1500
		}
		inbounds = append(inbounds, map[string]any{
			"type":           "tun",
			"tag":            "tun-in",
			"interface_name": iface,
			"mtu":            mtu,
		})
	}

	inboundType := p.name
	if p.name == "tun" {
		inboundType = "mixed"
	}
	serviceInbound := map[string]any{
		"type":        inboundType,
		"tag":         "service-in",
		"listen":      host,
		"listen_port": cfg.ListenPort,
	}
	if cfg.TLS.Enabled {
		serviceInbound["tls"] = map[string]any{
			"enabled":     true,
			"server_name": cfg.TLS.SNI,
			"alpn":        parseALPN(cfg.TLS.ALPN),
		}
	}

	inbounds = append(inbounds, serviceInbound)
	return &RenderResult{Inbounds: inbounds}, nil
}

func (p builtinProfile) Endpoint(cfg ProtocolConfig) EndpointMetadata {
	host := cfg.PublicEndpointHost
	port := cfg.PublicEndpointPort
	if port == 0 {
		port = cfg.ListenPort
	}
	transport := cfg.Transport
	if transport == "" {
		transport = p.name
	}
	return EndpointMetadata{
		Host:            host,
		Port:            port,
		Transport:       transport,
		ProtocolProfile: cfg.Profile,
		SNI:             cfg.TLS.SNI,
		ALPN:            cfg.TLS.ALPN,
		Ready:           host != "" && port > 0,
	}
}

func (p builtinProfile) HealthChecks(cfg ProtocolConfig) []HealthCheckSpec {
	return []HealthCheckSpec{{
		Name:     p.name + "-local-listener",
		Type:     "tcp",
		Target:   fmt.Sprintf("%s:%d", cfg.ListenHost, cfg.ListenPort),
		Required: true,
	}}
}

func (p builtinProfile) SecretRefs(cfg ProtocolConfig) []SecretRef {
	return RedactSecrets(cfg.Secrets)
}

func (p builtinProfile) Redact(cfg ProtocolConfig) ProtocolConfig {
	return RedactConfig(cfg)
}

func (p builtinProfile) SupportsClientConfig() bool {
	return false
}

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
