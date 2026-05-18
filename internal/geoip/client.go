package geoip

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/MyAiDevs/livemask-nodeagent/internal/agent"
)

// Client communicates with the Backend GeoIP API.
// Uses the existing NodeAgent HMAC request signing mechanism — no new keys.
// TASK-NODEAGENT-GEOIP-001.
type Client struct {
	httpClient *http.Client
	backendURL string
	nodeID     string
	nodeSecret string
	userAgent  string

	// maxDownloadBytes caps the response body size for package downloads.
	maxDownloadBytes int64
}

// ClientOption allows functional configuration of the GeoIP client.
type ClientOption func(*Client)

// WithDownloadLimit sets the maximum allowed download size in bytes.
func WithDownloadLimit(limit int64) ClientOption {
	return func(c *Client) {
		c.maxDownloadBytes = limit
	}
}

// WithHTTPClient sets a custom HTTP client (useful for testing).
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// NewClient creates a new GeoIP Backend API client.
// backendBaseURL is the base URL of the Backend, e.g. "http://backend:8080".
// nodeID and nodeSecret are used for HMAC request signing.
// userAgent is the agent version string sent as User-Agent header.
func NewClient(backendBaseURL, nodeID, nodeSecret, userAgent string, opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		backendURL:       backendBaseURL,
		nodeID:           nodeID,
		nodeSecret:       nodeSecret,
		userAgent:        userAgent,
		maxDownloadBytes: 100 * 1024 * 1024, // 100 MB default
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetNodeIdentity updates the node credentials used for request signing.
func (c *Client) SetNodeIdentity(nodeID, nodeSecret string) {
	c.nodeID = nodeID
	c.nodeSecret = nodeSecret
}

// Check queries the Backend for a GeoIP manifest update.
// GET /internal/agent/geoip/check?current_version=<version>&format=<format>&profile=<profile>
// See GEOIP_DATABASE_SYNC_CONTRACT.md §3.3.
func (c *Client) Check(ctx context.Context, currentVersion string, format Format, profile string) (*CheckResponse, error) {
	u, err := url.Parse(c.backendURL)
	if err != nil {
		return nil, fmt.Errorf("parse backend url: %w", err)
	}
	u.Path = path.Join(u.Path, "/internal/agent/geoip/check")
	q := u.Query()
	if currentVersion != "" {
		q.Set("current_version", currentVersion)
	}
	q.Set("format", string(format))
	q.Set("profile", profile)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create check request: %w", err)
	}
	c.setAuthHeaders(req)
	c.setCommonHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("check http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("check unexpected status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read check response: %w", err)
	}

	var checkResp CheckResponse
	if err := json.Unmarshal(body, &checkResp); err != nil {
		return nil, fmt.Errorf("unmarshal check response: %w", err)
	}
	return &checkResp, nil
}

// DownloadPackage downloads the GeoIP database package from the given URL.
// The URL must come from the Backend manifest — no third-party direct connects.
// Returns the downloaded bytes.
func (c *Client) DownloadPackage(ctx context.Context, packageURL string) ([]byte, error) {
	// Only accept http(s) URLs.
	parsed, err := url.Parse(packageURL)
	if err != nil {
		return nil, fmt.Errorf("parse package url: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("unsupported package url scheme: %s (only http/https allowed)", parsed.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, packageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	c.setCommonHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// Read with size limit.
	limited := io.LimitReader(resp.Body, c.maxDownloadBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read download body: %w", err)
	}
	if int64(len(data)) > c.maxDownloadBytes {
		return nil, fmt.Errorf("download exceeded max size %d bytes", c.maxDownloadBytes)
	}

	return data, nil
}

// setAuthHeaders sets the HMAC authentication headers on the request.
// Uses the same ComputeSignature from the agent package.
func (c *Client) setAuthHeaders(req *http.Request) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	req.Header.Set("X-Node-ID", c.nodeID)
	req.Header.Set("X-Timestamp", timestamp)
	sig := agent.ComputeSignature(c.nodeID, timestamp, c.nodeSecret)
	req.Header.Set("X-Signature", sig)
}

// setCommonHeaders sets shared request headers.
func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
}

// ReportEvent sends a GeoIP sync event to Backend.
// POST /internal/agent/geoip/events
// TASK-NODEAGENT-GEOIP-001, §3.4 of GEOIP_DATABASE_SYNC_CONTRACT.md.
func (c *Client) ReportEvent(ctx context.Context, event *SyncEvent) error {
	u, err := url.Parse(c.backendURL)
	if err != nil {
		return fmt.Errorf("parse backend url: %w", err)
	}
	u.Path = path.Join(u.Path, "/internal/agent/geoip/events")

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create event request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeaders(req)
	c.setCommonHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("event http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("event unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// SyncEvent represents a GeoIP sync event sent to Backend.
// See GEOIP_DATABASE_SYNC_CONTRACT.md §3.4.
type SyncEvent struct {
	RolloutID         string `json:"rollout_id,omitempty"`
	FromVersion       string `json:"from_version,omitempty"`
	ToVersion         string `json:"to_version"`
	Status            string `json:"status"`
	Reason            string `json:"reason,omitempty"`
	CurrentSHA256     string `json:"current_sha256,omitempty"`
	LastKnownGoodVersion string `json:"last_known_good_version,omitempty"`
	Message           string `json:"message,omitempty"`
}
