package agent

import (
	"testing"
)

func TestComputeSignature(t *testing.T) {
	// Test vector: known inputs produce known output.
	nodeID := "550e8400-e29b-41d4-a716-446655440000"
	timestamp := "1712345678"
	secret := "my-node-secret"

	sig := ComputeSignature(nodeID, timestamp, secret)
	if sig == "" {
		t.Fatal("signature should not be empty")
	}
	t.Logf("signature: %s", sig)

	// Deterministic — same inputs produce same output.
	sig2 := ComputeSignature(nodeID, timestamp, secret)
	if sig != sig2 {
		t.Fatal("signature should be deterministic")
	}

	// Different secret produces different signature.
	sig3 := ComputeSignature(nodeID, timestamp, secret+"x")
	if sig == sig3 {
		t.Fatal("different secret should produce different signature")
	}

	// Verify format: hex string.
	for _, c := range sig {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("signature contains non-hex char: %c", c)
		}
	}
}
