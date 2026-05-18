package protocol

import "strings"

const redactedValue = "<redacted>"

var sensitiveKeyFragments = []string{
	"password",
	"passwd",
	"pass",
	"private_key",
	"privatekey",
	"secret",
	"token",
	"psk",
	"auth",
	"key",
	"uuid",
	"node_secret",
	"hmac",
}

func RedactString(text string) string {
	if text == "" {
		return ""
	}
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == ';' || r == ')' || r == ']' || r == '\n' || r == '\t'
	})
	redacted := text
	for _, field := range fields {
		if !ContainsSensitiveMarker(field) {
			continue
		}
		if idx := strings.IndexAny(field, "=:"); idx >= 0 {
			replacement := field[:idx+1] + redactedValue
			redacted = strings.ReplaceAll(redacted, field, replacement)
			continue
		}
		redacted = strings.ReplaceAll(redacted, field, redactedValue)
	}
	return redacted
}

func RedactMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = RedactValue(k, v)
	}
	return out
}

func RedactProtocolConfig(cfg ProtocolConfig) ProtocolConfig {
	redacted := cfg
	redacted.Secrets = RedactSecretRefs(cfg.Secrets)
	redacted.Raw = RedactMap(cfg.Raw)
	if redacted.TLS.CertRef != "" {
		redacted.TLS.CertRef = redactedValue
	}
	if redacted.TLS.KeyRef != "" {
		redacted.TLS.KeyRef = redactedValue
	}
	return redacted
}

func RedactConfig(cfg ProtocolConfig) ProtocolConfig {
	return RedactProtocolConfig(cfg)
}

func RedactSecretRefs(secrets []SecretRef) []SecretRef {
	if len(secrets) == 0 {
		return nil
	}
	redacted := make([]SecretRef, len(secrets))
	for i, ref := range secrets {
		redacted[i] = ref
		if redacted[i].RedactionKey != "" {
			redacted[i].RedactionKey = redactedValue
		}
	}
	return redacted
}

func RedactSecrets(secrets []SecretRef) []SecretRef {
	return RedactSecretRefs(secrets)
}

func RedactValue(key string, value any) any {
	if isSensitiveKey(key) {
		return redactedValue
	}
	switch v := value.(type) {
	case map[string]any:
		return RedactMap(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = RedactValue("", v[i])
		}
		return out
	case string:
		if ContainsSensitiveMarker(v) {
			return RedactString(v)
		}
		return v
	default:
		return value
	}
}

func ContainsSensitiveMarker(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range sensitiveKeyFragments {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func ContainsSensitiveText(text string) bool {
	return ContainsSensitiveMarker(text)
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
