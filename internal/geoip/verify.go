package geoip

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// VerifySHA256 checks that the file at the given path matches the expected
// SHA-256 hex digest. The expected digest may optionally carry a "sha256:"
// prefix which is stripped before comparison. Returns nil on match or an
// error describing the failure.
// TASK-NODEAGENT-GEOIP-001.
func VerifySHA256(path, expected string) error {
	expected = stripSHA256Prefix(expected)

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file for sha256: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("read file for sha256: %w", err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("sha256 mismatch: got %s, expected %s", got, expected)
	}
	return nil
}

// ComputeSHA256Hex computes the SHA-256 hex digest of the file at path.
func ComputeSHA256Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for sha256: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read file for sha256: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ValidateSHA256Format checks that the expected string is a valid SHA-256
// hex digest (optionally with a "sha256:" prefix). Returns the raw hex
// string if valid, or an error if not.
// TASK-NODEAGENT-GEOIP-001.
func ValidateSHA256Format(expected string) (string, error) {
	raw := stripSHA256Prefix(expected)
	if len(raw) != 64 {
		return "", fmt.Errorf("invalid sha256 length: got %d, want 64", len(raw))
	}
	for _, c := range raw {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return "", fmt.Errorf("invalid sha256 hex character: %c", c)
		}
	}
	return raw, nil
}

// stripSHA256Prefix removes an optional "sha256:" prefix from the digest.
func stripSHA256Prefix(s string) string {
	if len(s) > 7 && s[:7] == "sha256:" {
		return s[7:]
	}
	return s
}
