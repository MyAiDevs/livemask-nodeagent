package protocol

import "fmt"

type ReservedProfile struct {
	name string
}

func (p ReservedProfile) Name() string { return p.name }

func (p ReservedProfile) Validate(ProtocolConfig) error {
	return fmt.Errorf("protocol profile %q is reserved but not implemented", p.name)
}

func (p ReservedProfile) Render(cfg ProtocolConfig) (*RenderResult, error) {
	if err := p.Validate(cfg); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("protocol profile %q is reserved but not implemented", p.name)
}

func (p ReservedProfile) Endpoint(cfg ProtocolConfig) EndpointMetadata {
	return endpointForProfile(p.name, cfg)
}

func (ReservedProfile) HealthChecks(ProtocolConfig) []HealthCheckSpec {
	return nil
}

func (ReservedProfile) SecretRefs(cfg ProtocolConfig) []SecretRef {
	return RedactSecretRefs(cfg.Secrets)
}

func (ReservedProfile) Redact(cfg ProtocolConfig) ProtocolConfig {
	return RedactProtocolConfig(cfg)
}

func (ReservedProfile) SupportsClientConfig() bool { return false }
