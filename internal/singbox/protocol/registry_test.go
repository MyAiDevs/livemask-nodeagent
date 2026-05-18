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
	profile, ok := Get("hysteria2")
	if !ok {
		t.Fatal("hysteria2 profile not registered")
	}
	err := profile.Validate(ProtocolConfig{Profile: "hysteria2", ListenPort: 443})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected not implemented, got %v", err)
	}
	_, err = profile.Render(ProtocolConfig{Profile: "hysteria2", ListenPort: 443})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected render not implemented, got %v", err)
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
