package protocol

type MixedProfile struct{}

func (MixedProfile) Name() string { return "mixed" }

func (p MixedProfile) Validate(cfg ProtocolConfig) error {
	return validateListenPort(p.Name(), cfg)
}

func (p MixedProfile) Render(cfg ProtocolConfig) (*RenderResult, error) {
	if err := p.Validate(cfg); err != nil {
		return nil, err
	}
	return &RenderResult{Inbounds: []map[string]any{serviceInbound("mixed", cfg)}}, nil
}

func (p MixedProfile) Endpoint(cfg ProtocolConfig) EndpointMetadata {
	return endpointForProfile(p.Name(), cfg)
}

func (p MixedProfile) HealthChecks(cfg ProtocolConfig) []HealthCheckSpec {
	return localTCPHealth(p.Name(), cfg, false)
}

func (MixedProfile) SecretRefs(cfg ProtocolConfig) []SecretRef {
	return RedactSecretRefs(cfg.Secrets)
}

func (MixedProfile) Redact(cfg ProtocolConfig) ProtocolConfig {
	return RedactProtocolConfig(cfg)
}

func (MixedProfile) SupportsClientConfig() bool { return false }
