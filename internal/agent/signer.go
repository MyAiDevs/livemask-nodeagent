package agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ComputeSignature computes the X-Signature header value for NodeAgent→Backend
// requests, matching Backend 02794f0's verification logic exactly.
//
// Backend stores node_secret_hash = HEX(SHA256(node_secret)) in the database.
// The server-side middleware (internal/node/middleware.go:74-78) recomputes:
//
//	expected = HEX(HMAC-SHA256(key=node_secret_hash, msg=nodeID + ":" + timestamp))
//
// where node_secret_hash is the hex-encoded SHA-256 string (as bytes).
//
// The NodeAgent MUST therefore do:
//
//	hashHex = HEX(SHA256(rawNodeSecret))
//	signature = HEX(HMAC-SHA256(key=[]byte(hashHex), msg=nodeID + ":" + timestamp))
func ComputeSignature(nodeID, timestamp, rawNodeSecret string) string {
	// Step 1: Compute SHA256(rawNodeSecret) and hex-encode.
	// This matches Backend HashSecret().
	digest := sha256.Sum256([]byte(rawNodeSecret))
	secretHashHex := hex.EncodeToString(digest[:])

	// Step 2: HMAC-SHA256 with the hex string as key bytes.
	// This matches Backend computeSignature().
	mac := hmac.New(sha256.New, []byte(secretHashHex))
	mac.Write([]byte(fmt.Sprintf("%s:%s", nodeID, timestamp)))
	return hex.EncodeToString(mac.Sum(nil))
}
