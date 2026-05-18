package geoip

import (
	"crypto/sha256"
	"fmt"
	"net/http"
)

// sha256Hex computes the SHA-256 hex digest of data (test helper).
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// helperServerURL constructs the base URL from an HTTP request for use
// in test handler closures.
func helperServerURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
