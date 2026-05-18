package protocol

import (
	"fmt"
	"strings"
	"testing"
)

type testProfile struct {
	name string
}

func (p testProfile) Name() string                  { return p.name }
func (p testProfile) Validate(ProtocolConfig) error { return nil }
func (p testProfile) Render(ProtocolConfig) (*RenderResult, error) {
	return &RenderResult{Inbounds: []map[string]any{{"type": "mixed", "tag": "test"}}}, nil
}
func (p testProfile) Endpoint(cfg ProtocolConfig) EndpointMetadata {
	return EndpointMetadata{Host: cfg.PublicEndpointHost, Port: cfg.PublicEndpointPort, ProtocolProfile: p.name, Ready: true}
}
func (p testProfile) HealthChecks(ProtocolConfig) []HealthCheckSpec {
	return []HealthCheckSpec{{Name: "test", Type: "tcp", Required: true}}
}
func (p testProfile) SecretRefs(cfg ProtocolConfig) []SecretRef { return cfg.Secrets }
func (p testProfile) Redact(cfg ProtocolConfig) ProtocolConfig  { return RedactConfig(cfg) }
func (p testProfile) SupportsClientConfig() bool                { return true }

func TestRegistry_RegisterGetUnknown(t *testing.T) {
	registry := NewRegistry()
	profile := testProfile{name: "fake"}
	if err := registry.Register(profile); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := registry.Get("fake")
	if !ok {
		t.Fatal("expected fake profile")
	}
	if got.Name() != "fake" {
		t.Fatalf("expected fake, got %s", got.Name())
	}
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("expected unknown profile to be absent")
	}
}

func TestRegistry_RegisterRejectsDuplicate(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(testProfile{name: "fake"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := registry.Register(testProfile{name: "fake"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestResolveProfileName_LegacyDefaultsToTransport(t *testing.T) {
	got := ResolveProfileName(ProtocolConfig{Profile: "tcp_udp", Transport: "socks"})
	if got != "socks" {
		t.Fatalf("expected socks, got %s", got)
	}
	got = NormalizeProfileName("singbox")
	if got != "mixed" {
		t.Fatalf("expected singbox alias to mixed, got %s", got)
	}
	got = NormalizeProfileName("")
	if got != "mixed" {
		t.Fatalf("expected empty alias to mixed, got %s", got)
	}
	got = ResolveProfileName(ProtocolConfig{})
	if got != DefaultProfileName {
		t.Fatalf("expected default profile %s, got %s", DefaultProfileName, got)
	}
}

func TestRedactConfig(t *testing.T) {
	cfg := ProtocolConfig{
		Secrets: []SecretRef{{Name: "node-token", Source: "env"}},
		Raw: map[string]any{
			"password":    "plain",
			"private_key": "plain",
			"nested": map[string]any{
				"api_token": "plain",
				"public":    "ok",
			},
		},
	}

	redacted := RedactConfig(cfg)
	rawText := fmt.Sprintf("%v", redacted.Raw)
	for _, secret := range []string{"password:plain", "private_key:plain", "api_token:plain"} {
		if strings.Contains(rawText, secret) {
			t.Fatalf("expected secret redacted, got %s", rawText)
		}
	}
	if redacted.Raw["password"] != "<redacted>" {
		t.Fatalf("expected password redacted, got %v", redacted.Raw["password"])
	}
	if redacted.Secrets[0].RedactionKey != "" {
		t.Fatalf("expected empty redaction key to stay metadata-only, got %s", redacted.Secrets[0].RedactionKey)
	}
	cfg.Secrets[0].RedactionKey = "token"
	redacted = RedactConfig(cfg)
	if redacted.Secrets[0].RedactionKey != "<redacted>" {
		t.Fatalf("expected redaction key, got %s", redacted.Secrets[0].RedactionKey)
	}
}

func TestDefaultRegistryProfiles(t *testing.T) {
	registry := DefaultRegistry()
	for _, name := range []string{"mixed", "socks", "tun", "hysteria2", "vless_reality", "trojan", "shadowtls", "wireguard"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("expected profile %s to be registered", name)
		}
	}
}

func TestReservedProfileNotImplemented(t *testing.T) {
	for _, name := range []string{"vless_reality", "trojan", "shadowtls", "wireguard"} {
		profile, ok := Get(name)
		if !ok {
			t.Fatalf("%s profile not registered", name)
		}
		err := profile.Validate(ProtocolConfig{Profile: name, ListenPort: 443})
		if err == nil || !strings.Contains(err.Error(), "not implemented") {
			t.Fatalf("profile %q expected not implemented, got %v", name, err)
		}
		_, err = profile.Render(ProtocolConfig{Profile: name, ListenPort: 443})
		if err == nil || !strings.Contains(err.Error(), "not implemented") {
			t.Fatalf("profile %q expected render not implemented, got %v", name, err)
		}
	}
}

func TestBuiltinEndpointAndHealthChecks(t *testing.T) {
	profile, ok := Get("mixed")
	if !ok {
		t.Fatal("mixed profile not registered")
	}
	cfg := ProtocolConfig{
		Profile:            "mixed",
		Transport:          "mixed",
		ListenHost:         "127.0.0.1",
		ListenPort:         10808,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 443,
		TLS:                TLSConfig{SNI: "node.example.com", ALPN: "h2"},
	}
	ep := profile.Endpoint(cfg)
	if ep.Host != "node.example.com" || ep.Port != 443 || ep.ProtocolProfile != "mixed" {
		t.Fatalf("unexpected endpoint metadata: %+v", ep)
	}
	checks := profile.HealthChecks(cfg)
	if len(checks) != 1 || checks[0].Name == "" || !checks[0].Required {
		t.Fatalf("unexpected health checks: %+v", checks)
	}
}

func TestHysteria2_GetAndName(t *testing.T) {
	profile, ok := Get("hysteria2")
	if !ok {
		t.Fatal("hysteria2 profile not registered")
	}
	if got := profile.Name(); got != "hysteria2" {
		t.Fatalf("expected name hysteria2, got %s", got)
	}
	names := DefaultRegistry().Names()
	found := false
	for _, n := range names {
		if n == "hysteria2" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Names() should contain hysteria2")
	}
	got := NormalizeProfileName("hysteria2")
	if got != "hysteria2" {
		t.Fatalf("NormalizeProfileName(hysteria2) = %s, want hysteria2", got)
	}
}

func TestHysteria2_Validate_Valid(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")

	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
	}
	profile, _ := Get("hysteria2")
	err := profile.Validate(cfg)
	if err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}
}

func TestHysteria2_Validate_MissingPublicEndpoint(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")

	cfg := ProtocolConfig{
		Profile:    "hysteria2",
		Transport:  "udp",
		ListenHost: "127.0.0.1",
		ListenPort: 443,
	}
	profile, _ := Get("hysteria2")
	err := profile.Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing public_endpoint_host")
	}
}

func TestHysteria2_Validate_InvalidPort(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")

	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         0,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
	}
	profile, _ := Get("hysteria2")
	err := profile.Validate(cfg)
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestHysteria2_Validate_MissingAuthSecret(t *testing.T) {
	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
	}
	profile, _ := Get("hysteria2")
	err := profile.Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing auth secret")
	}
	if strings.Contains(err.Error(), "HYSTERIA2_AUTH=") || strings.Contains(err.Error(), "test-auth-value") {
		t.Fatal("error should not contain env value")
	}
}

func TestHysteria2_Validate_ObfsMissingPassword(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")

	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
		Raw: map[string]any{
			"hysteria2_obfs_type": "obfs",
		},
	}
	profile, _ := Get("hysteria2")
	err := profile.Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing obfs password when obfs enabled")
	}
}

func TestHysteria2_Validate_SecretsFromRaw(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")
	t.Setenv("HYSTERIA2_OBFS_PASSWORD", "test-obfs-value")

	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
		Raw: map[string]any{
			"hysteria2_obfs_type":       "obfs",
			"hysteria2_auth_env":        "HYSTERIA2_AUTH",
			"hysteria2_obfs_password_env": "HYSTERIA2_OBFS_PASSWORD",
		},
	}
	profile, _ := Get("hysteria2")
	err := profile.Validate(cfg)
	if err != nil {
		t.Fatalf("expected valid config with env secrets to pass, got: %v", err)
	}
}

func TestHysteria2_Render_ValidJSON(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")

	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
		TLS: TLSConfig{
			Enabled: true,
			SNI:     "node.example.com",
			ALPN:    "h2",
		},
	}
	profile, _ := Get("hysteria2")
	result, err := profile.Render(cfg)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if result == nil {
		t.Fatal("Render returned nil result")
	}
	if len(result.Inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(result.Inbounds))
	}
	inbound := result.Inbounds[0]
	if inbound["type"] != "hysteria2" {
		t.Fatalf("expected type hysteria2, got %v", inbound["type"])
	}
	if inbound["listen"] != "127.0.0.1" {
		t.Fatalf("expected listen 127.0.0.1, got %v", inbound["listen"])
	}
	if inbound["listen_port"] != 443 {
		t.Fatalf("expected listen_port 443, got %v", inbound["listen_port"])
	}
	_, hasHysteria2 := inbound["hysteria2"]
	if !hasHysteria2 {
		t.Fatal("expected hysteria2 config block")
	}
	_, hasTLS := inbound["tls"]
	if !hasTLS {
		t.Fatal("expected tls config block")
	}
}

func TestHysteria2_Render_NoSecretInOutput(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")

	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
	}
	profile, _ := Get("hysteria2")
	result, err := profile.Render(cfg)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	inbound := result.Inbounds[0]
	hy2Config, ok := inbound["hysteria2"].(map[string]any)
	if !ok {
		t.Fatal("expected hysteria2 config as map")
	}
	auth, ok := hy2Config["auth"]
	if !ok {
		t.Fatal("expected auth in hysteria2 config")
	}
	authStr, ok := auth.(string)
	if !ok {
		t.Fatal("expected auth as string")
	}
	if authStr == "" {
		t.Fatal("expected non-empty auth value")
	}
}

func TestHysteria2_Render_WithObfs(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")
	t.Setenv("HYSTERIA2_OBFS_PASSWORD", "obfs-password-value")

	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
		Raw: map[string]any{
			"hysteria2_obfs_type": "obfs",
		},
	}
	profile, _ := Get("hysteria2")
	result, err := profile.Render(cfg)
	if err != nil {
		t.Fatalf("Render with obfs failed: %v", err)
	}
	inbound := result.Inbounds[0]
	hy2Config := inbound["hysteria2"].(map[string]any)
	obfs, ok := hy2Config["obfs"].(map[string]any)
	if !ok {
		t.Fatal("expected obfs config block")
	}
	if obfs["type"] != "obfs" {
		t.Fatalf("expected obfs type obfs, got %v", obfs["type"])
	}
	if obfs["password"] != "obfs-password-value" {
		t.Fatalf("unexpected obfs password: got %v", obfs["password"])
	}
}

func TestHysteria2_Render_NoSecret(t *testing.T) {
	// Test that render fails when no auth secret is available.
	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
	}
	profile, _ := Get("hysteria2")
	_, err := profile.Render(cfg)
	if err == nil {
		t.Fatal("expected error for missing auth secret")
	}
	if strings.Contains(err.Error(), "test-auth-value") {
		t.Fatal("error should not contain secret value")
	}
}

func TestHysteria2_Endpoint(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")

	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
		TLS: TLSConfig{
			Enabled: true,
			SNI:     "sni.example.com",
			ALPN:    "h2,http/1.1",
		},
	}
	profile, _ := Get("hysteria2")
	ep := profile.Endpoint(cfg)
	if ep.ProtocolProfile != "hysteria2" {
		t.Fatalf("expected protocol_profile hysteria2, got %s", ep.ProtocolProfile)
	}
	if ep.Transport != "udp" {
		t.Fatalf("expected transport udp, got %s", ep.Transport)
	}
	if ep.Host != "node.example.com" {
		t.Fatalf("expected host node.example.com, got %s", ep.Host)
	}
	if ep.Port != 8443 {
		t.Fatalf("expected port 8443, got %d", ep.Port)
	}
	if ep.SNI != "sni.example.com" {
		t.Fatalf("expected SNI sni.example.com, got %s", ep.SNI)
	}
	if !ep.Ready {
		t.Fatal("expected endpoint ready")
	}
}

func TestHysteria2_HealthChecks(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")

	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
	}
	profile, _ := Get("hysteria2")
	checks := profile.HealthChecks(cfg)
	if len(checks) < 1 {
		t.Fatal("expected at least 1 health check")
	}
	hasTCP := false
	hasUDP := false
	for _, c := range checks {
		if c.Type == "tcp" {
			hasTCP = true
		}
		if c.Type == "udp" {
			hasUDP = true
		}
	}
	if !hasTCP {
		t.Fatal("expected TCP health check")
	}
	if !hasUDP {
		t.Fatal("expected UDP health check for public endpoint")
	}
}

func TestHysteria2_Redact(t *testing.T) {
	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
		Secrets: []SecretRef{
			{Name: "hysteria2-auth", Source: "env", Required: true, RedactionKey: "auth"},
		},
		Raw: map[string]any{
			"hysteria2_auth_env":          "HYSTERIA2_AUTH",
			"hysteria2_obfs_password_env": "HYSTERIA2_OBFS_PASSWORD",
			"password":                    "plain",
			"private_key":                 "plain",
			"nested": map[string]any{
				"api_token": "plain",
				"public":    "ok",
			},
		},
	}
	profile, _ := Get("hysteria2")
	redacted := profile.Redact(cfg)

	// Verify hysteria2 env fields are redacted.
	if redacted.Raw["hysteria2_auth_env"] != "<redacted>" {
		t.Fatalf("expected hysteria2_auth_env to be redacted, got %v", redacted.Raw["hysteria2_auth_env"])
	}
	if redacted.Raw["hysteria2_obfs_password_env"] != "<redacted>" {
		t.Fatalf("expected hysteria2_obfs_password_env to be redacted, got %v", redacted.Raw["hysteria2_obfs_password_env"])
	}
	// Verify standard fields still redacted.
	if redacted.Raw["password"] != "<redacted>" {
		t.Fatalf("expected password redacted, got %v", redacted.Raw["password"])
	}
	if redacted.Raw["private_key"] != "<redacted>" {
		t.Fatalf("expected private_key redacted, got %v", redacted.Raw["private_key"])
	}
	// Verify nested map redaction.
	nested := redacted.Raw["nested"].(map[string]any)
	if nested["api_token"] != "<redacted>" {
		t.Fatalf("expected api_token redacted, got %v", nested["api_token"])
	}
	if nested["public"] != "ok" {
		t.Fatalf("expected public to stay 'ok', got %v", nested["public"])
	}
}

func TestHysteria2_SecretRefs(t *testing.T) {
	cfg := ProtocolConfig{
		Secrets: []SecretRef{
			{Name: "existing-secret", Source: "env", Required: true},
		},
		Raw: map[string]any{
			"hysteria2_auth_env":          "HYSTERIA2_AUTH",
			"hysteria2_obfs_password_env": "HYSTERIA2_OBFS_PASSWORD",
		},
	}
	profile, _ := Get("hysteria2")
	refs := profile.SecretRefs(cfg)
	foundAuth := false
	foundObfs := false
	for _, ref := range refs {
		if ref.Name == "hysteria2_auth" {
			foundAuth = true
		}
		if ref.Name == "hysteria2_obfs_password" {
			foundObfs = true
		}
	}
	if !foundAuth {
		t.Fatal("expected hysteria2_auth SecretRef")
	}
	if !foundObfs {
		t.Fatal("expected hysteria2_obfs_password SecretRef")
	}
}

func TestHysteria2_ErrorDoesNotLeakSecret(t *testing.T) {
	cfg := ProtocolConfig{
		Profile:            "hysteria2",
		Transport:          "udp",
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
	}
	profile, _ := Get("hysteria2")
	err := profile.Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing auth")
	}
	errStr := err.Error()
	for _, secret := range []string{"HYSTERIA2_AUTH=", "test-auth-value", "password=test"} {
		if strings.Contains(errStr, secret) {
			t.Fatalf("error should not contain secret: %s", secret)
		}
	}
	// Verify it contains the expected message.
	if !strings.Contains(errStr, "auth secret is required") {
		t.Fatalf("expected 'auth secret is required' in error, got: %s", errStr)
	}
}
