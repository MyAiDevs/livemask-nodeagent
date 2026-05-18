package protocol

import (
	"fmt"
	"strings"
)

func init() {
	for _, profile := range []ProtocolProfile{
		MixedProfile{},
		SocksProfile{},
		TunProfile{},
		ReservedProfile{name: "hysteria2"},
		ReservedProfile{name: "vless_reality"},
		ReservedProfile{name: "trojan"},
		ReservedProfile{name: "shadowtls"},
		ReservedProfile{name: "wireguard"},
	} {
		if err := Register(profile); err != nil {
			panic(err)
		}
	}
}

func validateListenPort(profile string, cfg ProtocolConfig) error {
	if cfg.ListenPort <= 0 || cfg.ListenPort > 65535 {
		return fmt.Errorf("protocol profile %q listen port is invalid", profile)
	}
	return nil
}

func serviceInbound(inboundType string, cfg ProtocolConfig) map[string]any {
	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}
	inbound := map[string]any{
		"type":        inboundType,
		"tag":         "service-in",
		"listen":      host,
		"listen_port": cfg.ListenPort,
	}
	if cfg.TLS.Enabled {
		inbound["tls"] = map[string]any{
			"enabled":     true,
			"server_name": firstNonEmptyString(cfg.TLS.SNI, cfg.SNI),
			"alpn":        parseALPN(firstNonEmptyString(cfg.TLS.ALPN, cfg.ALPN)),
		}
	}
	return inbound
}

func endpointForProfile(profile string, cfg ProtocolConfig) EndpointMetadata {
	port := cfg.PublicEndpointPort
	if port == 0 {
		port = cfg.ListenPort
	}
	transport := cfg.Transport
	if transport == "" {
		transport = profile
	}
	host := cfg.PublicEndpointHost
	return EndpointMetadata{
		Host:            host,
		Port:            port,
		Transport:       transport,
		ProtocolProfile: profile,
		SNI:             firstNonEmptyString(cfg.TLS.SNI, cfg.SNI),
		ALPN:            firstNonEmptyString(cfg.TLS.ALPN, cfg.ALPN),
		Ready:           host != "" && port > 0 && port <= 65535,
	}
}

func localTCPHealth(profile string, cfg ProtocolConfig, optional bool) []HealthCheckSpec {
	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}
	timeout := 5000
	return []HealthCheckSpec{{
		Name:      profile + "-local-listener",
		Type:      "tcp",
		Host:      host,
		Port:      cfg.ListenPort,
		TimeoutMS: timeout,
		Optional:  optional,
		Target:    fmt.Sprintf("%s:%d", host, cfg.ListenPort),
		Required:  !optional,
		TimeoutMs: timeout,
	}}
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
