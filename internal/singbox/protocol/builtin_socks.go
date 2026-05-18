package protocol

type SocksProfile struct{}

func (SocksProfile) Name() string { return "socks" }

func (p SocksProfile) Validate(cfg ProtocolConfig) error {
	return validateListenPort(p.Name(), cfg)
}

func (p SocksProfile) Render(cfg ProtocolConfig) (*RenderResult, error) {
	if err := p.Validate(cfg); err != nil {
		return nil, err
	}
	return &RenderResult{Inbounds: []map[string]any{serviceInbound("socks", cfg)}}, nil
}

func (p SocksProfile) Endpoint(cfg ProtocolConfig) EndpointMetadata {
	return endpointForProfile(p.Name(), cfg)
}

func (p SocksProfile) HealthChecks(cfg ProtocolConfig) []HealthCheckSpec {
	return localTCPHealth(p.Name(), cfg, false)
}

func (SocksProfile) SecretRefs(cfg ProtocolConfig) []SecretRef {
	return RedactSecretRefs(cfg.Secrets)
}

func (SocksProfile) Redact(cfg ProtocolConfig) ProtocolConfig {
	return RedactProtocolConfig(cfg)
}

func (SocksProfile) SupportsClientConfig() bool { return false }
