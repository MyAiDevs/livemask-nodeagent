package protocol

import "fmt"

type TunProfile struct{}

func (TunProfile) Name() string { return "tun" }

func (p TunProfile) Validate(cfg ProtocolConfig) error {
	if err := validateListenPort(p.Name(), cfg); err != nil {
		return err
	}
	if mtu, ok := cfg.Raw["tun_mtu"].(int); ok && mtu > 0 && mtu < 128 {
		return fmt.Errorf("protocol profile %q tun_mtu must be >= 128", p.Name())
	}
	return nil
}

func (p TunProfile) Render(cfg ProtocolConfig) (*RenderResult, error) {
	if err := p.Validate(cfg); err != nil {
		return nil, err
	}
	if cfg.Raw == nil {
		cfg.Raw = map[string]any{}
	}
	iface, _ := cfg.Raw["tun_interface_name"].(string)
	if iface == "" {
		iface = "singbox-tun0"
	}
	mtu, _ := cfg.Raw["tun_mtu"].(int)
	if mtu <= 0 {
		mtu = 1500
	}
	return &RenderResult{Inbounds: []map[string]any{
		{
			"type":           "tun",
			"tag":            "tun-in",
			"interface_name": iface,
			"mtu":            mtu,
		},
		serviceInbound("mixed", cfg),
	}}, nil
}

func (p TunProfile) Endpoint(cfg ProtocolConfig) EndpointMetadata {
	return endpointForProfile(p.Name(), cfg)
}

func (p TunProfile) HealthChecks(cfg ProtocolConfig) []HealthCheckSpec {
	return localTCPHealth(p.Name(), cfg, true)
}

func (TunProfile) SecretRefs(cfg ProtocolConfig) []SecretRef {
	return RedactSecretRefs(cfg.Secrets)
}

func (TunProfile) Redact(cfg ProtocolConfig) ProtocolConfig {
	return RedactProtocolConfig(cfg)
}

func (TunProfile) SupportsClientConfig() bool { return false }
