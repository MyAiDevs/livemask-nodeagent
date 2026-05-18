package protocol

import (
	"fmt"
	"os"
	"strings"
)

// Hysteria2Profile implements ProtocolProfile for the hysteria2 protocol.
// TASK-NODEAGENT-HYSTERIA2-001.
type Hysteria2Profile struct{}

func (Hysteria2Profile) Name() string { return "hysteria2" }

func (p Hysteria2Profile) Validate(cfg ProtocolConfig) error {
	// Public endpoint is required for hysteria2 (it's a server protocol).
	if cfg.PublicEndpointHost == "" {
		return fmt.Errorf("protocol profile %q public_endpoint_host is required", p.Name())
	}
	if cfg.PublicEndpointPort <= 0 || cfg.PublicEndpointPort > 65535 {
		return fmt.Errorf("protocol profile %q public_endpoint_port %d is invalid (must be 1-65535)", p.Name(), cfg.PublicEndpointPort)
	}

	// Listen port is required.
	if cfg.ListenPort <= 0 || cfg.ListenPort > 65535 {
		return fmt.Errorf("protocol profile %q listen_port %d is invalid (must be 1-65535)", p.Name(), cfg.ListenPort)
	}

	// Transport must be udp for hysteria2 (default to udp if empty).
	transport := strings.ToLower(cfg.Transport)
	if transport == "" {
		transport = "udp"
	}
	if transport != "udp" {
		return fmt.Errorf("protocol profile %q transport must be \"udp\", got %q", p.Name(), cfg.Transport)
	}

	// Auth secret is required. Check SecretRefs or env var.
	if err := p.requireAuthSecret(cfg); err != nil {
		return err
	}

	// Validate obfs type if set.
	if obfsType := getObfsType(cfg); obfsType != "" {
		validObfs := map[string]bool{
			"":        true,
			"obfs":    true,
			"obfs_tls": true,
		}
		if !validObfs[obfsType] {
			return fmt.Errorf("protocol profile %q invalid obfs_type %q, must be \"obfs\" or \"obfs_tls\"", p.Name(), obfsType)
		}
		// If obfs is enabled, obfs_password is required.
		if err := p.requireObfsPasswordSecret(cfg); err != nil {
			return err
		}
	}

	// Validate up/down Mbps if set.
	if upMbps := getUpMbps(cfg); upMbps < 0 {
		return fmt.Errorf("protocol profile %q up_mbps %d is invalid (must be >= 0)", p.Name(), upMbps)
	}
	if downMbps := getDownMbps(cfg); downMbps < 0 {
		return fmt.Errorf("protocol profile %q down_mbps %d is invalid (must be >= 0)", p.Name(), downMbps)
	}

	return nil
}

func (p Hysteria2Profile) Render(cfg ProtocolConfig) (*RenderResult, error) {
	if err := p.Validate(cfg); err != nil {
		return nil, err
	}

	// Resolve secrets from env vars if referenced.
	authSecret, err := resolveAuthSecret(cfg)
	if err != nil {
		return nil, fmt.Errorf("protocol profile %q: %w", p.Name(), err)
	}

	// Build listen host.
	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}

	// Build the hysteria2 inbound.
	inbound := map[string]any{
		"type":        "hysteria2",
		"tag":         "hy2-in",
		"listen":      host,
		"listen_port": cfg.ListenPort,
	}

	// Add TLS config if enabled.
	if cfg.TLS.Enabled {
		tlsMap := map[string]any{
			"enabled": true,
		}
		if sni := firstNonEmptyString(cfg.TLS.SNI, cfg.SNI); sni != "" {
			tlsMap["server_name"] = sni
		}
		if alpn := parseALPN(firstNonEmptyString(cfg.TLS.ALPN, cfg.ALPN)); len(alpn) > 0 {
			tlsMap["alpn"] = alpn
		}
		inbound["tls"] = tlsMap
	}

	// Build the hysteria2-specific config.
	hy2Config := map[string]any{
		"auth": authSecret,
	}

	if upMbps := getUpMbps(cfg); upMbps > 0 {
		hy2Config["up_mbps"] = upMbps
	}
	if downMbps := getDownMbps(cfg); downMbps > 0 {
		hy2Config["down_mbps"] = downMbps
	}

	// Obfuscation.
	obfsType := getObfsType(cfg)
	if obfsType != "" {
		obfsPassword, err := resolveObfsPassword(cfg)
		if err != nil {
			return nil, fmt.Errorf("protocol profile %q: %w", p.Name(), err)
		}
		hy2Config["obfs"] = map[string]any{
			"type":     obfsType,
			"password": obfsPassword,
		}
	}

	inbound["hysteria2"] = hy2Config

	return &RenderResult{
		Inbounds:  []map[string]any{inbound},
		Outbounds: nil, // will use default outbounds from renderer
		Route:     nil, // will use default route from renderer
		DNS:       nil, // will use default DNS from renderer
	}, nil
}

func (p Hysteria2Profile) Endpoint(cfg ProtocolConfig) EndpointMetadata {
	return endpointForProfile(p.Name(), cfg)
}

func (p Hysteria2Profile) HealthChecks(cfg ProtocolConfig) []HealthCheckSpec {
	// Use TCP health check for listen port (even though hysteria2 uses UDP,
	// TCP listener check still validates the process is alive).
	tcpCheck := localTCPHealth(p.Name(), cfg, false)
	// Add a UDP health check spec for the public endpoint.
	checks := []HealthCheckSpec{
		{
			Name:      p.Name() + "-public-udp",
			Type:      "udp",
			Host:      cfg.PublicEndpointHost,
			Port:      cfg.PublicEndpointPort,
			TimeoutMS: 5000,
			Optional:  true, // UDP public probe is best-effort
			Target:    fmt.Sprintf("%s:%d", cfg.PublicEndpointHost, cfg.PublicEndpointPort),
			Required:  false,
			TimeoutMs: 5000,
		},
	}
	checks = append(checks, tcpCheck...)
	return checks
}

func (Hysteria2Profile) SecretRefs(cfg ProtocolConfig) []SecretRef {
	refs := RedactSecretRefs(cfg.Secrets)
	// Add implicit SecretRefs for env-based secrets.
	if authEnv := getAuthEnv(cfg); authEnv != "" {
		refs = append(refs, SecretRef{
			Name:     "hysteria2_auth",
			Source:   "env",
			Required: true,
		})
	}
	if obfsPasswordEnv := getObfsPasswordEnv(cfg); obfsPasswordEnv != "" {
		refs = append(refs, SecretRef{
			Name:     "hysteria2_obfs_password",
			Source:   "env",
			Required: true,
		})
	}
	return refs
}

func (Hysteria2Profile) Redact(cfg ProtocolConfig) ProtocolConfig {
	redacted := RedactProtocolConfig(cfg)
	// Redact hysteria2-specific Raw fields.
	if redacted.Raw != nil {
		for _, key := range []string{"hysteria2_auth_env", "hysteria2_obfs_password_env"} {
			if v, ok := redacted.Raw[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					redacted.Raw[key] = redactedValue
				}
			}
		}
	}
	return redacted
}

func (Hysteria2Profile) SupportsClientConfig() bool { return false }

// ---- hysteria2-specific helpers ----

func getUpMbps(cfg ProtocolConfig) int {
	if cfg.Raw == nil {
		return 0
	}
	if v, ok := cfg.Raw["hysteria2_up_mbps"]; ok {
		if n, ok := v.(int); ok {
			return n
		}
	}
	return 0
}

func getDownMbps(cfg ProtocolConfig) int {
	if cfg.Raw == nil {
		return 0
	}
	if v, ok := cfg.Raw["hysteria2_down_mbps"]; ok {
		if n, ok := v.(int); ok {
			return n
		}
	}
	return 0
}

func getObfsType(cfg ProtocolConfig) string {
	if cfg.Raw == nil {
		return ""
	}
	if v, ok := cfg.Raw["hysteria2_obfs_type"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getAuthEnv(cfg ProtocolConfig) string {
	if cfg.Raw == nil {
		return ""
	}
	if v, ok := cfg.Raw["hysteria2_auth_env"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getObfsPasswordEnv(cfg ProtocolConfig) string {
	if cfg.Raw == nil {
		return ""
	}
	if v, ok := cfg.Raw["hysteria2_obfs_password_env"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (p Hysteria2Profile) requireAuthSecret(cfg ProtocolConfig) error {
	// Check explicit SecretRefs first.
	for _, ref := range cfg.Secrets {
		lower := strings.ToLower(ref.Name)
		if strings.Contains(lower, "auth") || strings.Contains(lower, "password") {
			return nil
		}
	}
	// Check env var reference.
	authEnv := getAuthEnv(cfg)
	if authEnv != "" {
		val := os.Getenv(authEnv)
		if val != "" {
			return nil
		}
		return fmt.Errorf("protocol profile %q auth secret env %q is set but empty", p.Name(), authEnv)
	}
	// Check inline env var names.
	for _, key := range []string{"HYSTERIA2_AUTH", "HYSTERIA2_AUTH_PASSWORD", "HYSTERIA2_OBFS_PASSWORD"} {
		if v := os.Getenv(key); v != "" {
			return nil
		}
	}
	return errMissingAuth(p.Name())
}

func (p Hysteria2Profile) requireObfsPasswordSecret(cfg ProtocolConfig) error {
	obfsPasswordEnv := getObfsPasswordEnv(cfg)
	if obfsPasswordEnv != "" {
		val := os.Getenv(obfsPasswordEnv)
		if val != "" {
			return nil
		}
		return fmt.Errorf("protocol profile %q obfs password env %q is set but empty", p.Name(), obfsPasswordEnv)
	}
	// Check default env var.
	if v := os.Getenv("HYSTERIA2_OBFS_PASSWORD"); v != "" {
		return nil
	}
	// If we have explicit SecretRefs with obfs-related names.
	for _, ref := range cfg.Secrets {
		lower := strings.ToLower(ref.Name)
		if strings.Contains(lower, "obfs") || strings.Contains(lower, "obfuscate") {
			return nil
		}
	}
	return errMissingObfsPassword(p.Name())
}

func resolveAuthSecret(cfg ProtocolConfig) (string, error) {
	// Try explicit SecretRefs first.
	for _, ref := range cfg.Secrets {
		lower := strings.ToLower(ref.Name)
		if strings.Contains(lower, "auth") || strings.Contains(lower, "password") {
			return ref.Name, nil // In a real system this would resolve from vault
		}
	}
	// Try env var reference.
	authEnv := getAuthEnv(cfg)
	if authEnv != "" {
		val := os.Getenv(authEnv)
		if val != "" {
			return val, nil
		}
		return "", errMissingAuth("hysteria2")
	}
	// Try known env var names.
	for _, key := range []string{"HYSTERIA2_AUTH", "HYSTERIA2_AUTH_PASSWORD"} {
		if v := os.Getenv(key); v != "" {
			return v, nil
		}
	}
	// Last fallback: try HYSTERIA2_OBFS_PASSWORD as auth (some deployments use same secret).
	if v := os.Getenv("HYSTERIA2_OBFS_PASSWORD"); v != "" {
		return v, nil
	}
	return "", errMissingAuth("hysteria2")
}

func resolveObfsPassword(cfg ProtocolConfig) (string, error) {
	obfsPasswordEnv := getObfsPasswordEnv(cfg)
	if obfsPasswordEnv != "" {
		val := os.Getenv(obfsPasswordEnv)
		if val != "" {
			return val, nil
		}
		return "", errMissingObfsPassword("hysteria2")
	}
	// Try default env var.
	if v := os.Getenv("HYSTERIA2_OBFS_PASSWORD"); v != "" {
		return v, nil
	}
	// Try SecretRefs.
	for _, ref := range cfg.Secrets {
		lower := strings.ToLower(ref.Name)
		if strings.Contains(lower, "obfs") || strings.Contains(lower, "obfuscate") {
			return ref.Name, nil
		}
	}
	return "", errMissingObfsPassword("hysteria2")
}

// ---- error constructors ----

func errMissingAuth(profile string) error {
	return fmt.Errorf("protocol profile %q auth secret is required (set HYSTERIA2_AUTH env or SecretRef)", profile)
}

func errMissingObfsPassword(profile string) error {
	return fmt.Errorf("protocol profile %q obfs password is required when obfs is enabled (set HYSTERIA2_OBFS_PASSWORD env or SecretRef)", profile)
}
