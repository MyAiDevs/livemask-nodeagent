package protocol

import "strings"

var sensitiveKeyFragments = []string{
	"password",
	"passwd",
	"token",
	"secret",
	"private_key",
	"privatekey",
	"access_key",
	"key",
}

func RedactConfig(cfg ProtocolConfig) ProtocolConfig {
	redacted := cfg
	redacted.Secrets = RedactSecrets(cfg.Secrets)
	redacted.Raw = redactMap(cfg.Raw)
	return redacted
}

func RedactSecrets(secrets []SecretRef) []SecretRef {
	if len(secrets) == 0 {
		return nil
	}
	redacted := make([]SecretRef, len(secrets))
	for i, ref := range secrets {
		redacted[i] = ref
		if redacted[i].RedactionKey == "" {
			redacted[i].RedactionKey = "[redacted]"
		}
	}
	return redacted
}

func RedactValue(key string, value any) any {
	if isSensitiveKey(key) {
		return "[redacted]"
	}
	switch v := value.(type) {
	case map[string]any:
		return redactMap(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = RedactValue("", v[i])
		}
		return out
	default:
		return value
	}
}

func ContainsSensitiveText(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range sensitiveKeyFragments {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func redactMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = RedactValue(k, v)
	}
	return out
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range sensitiveKeyFragments {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
