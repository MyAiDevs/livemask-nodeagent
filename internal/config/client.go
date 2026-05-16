package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client fetches nodeagent runtime config from the Backend config centre.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	nodeID       string
	agentVersion string
}

// ClientOption allows functional configuration of the Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client (useful for testing).
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cl *Client) {
		cl.httpClient = c
	}
}

// NewClient creates a new config Client.
// baseURL should point to the Backend internal endpoint, e.g.
// "http://backend:8080/internal/agent/config".
func NewClient(baseURL, nodeID, agentVersion string, opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL:      baseURL,
		nodeID:       nodeID,
		agentVersion: agentVersion,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Fetch retrieves the latest config from Backend.
// If localVersion > 0 it is sent as the local last-known-good version.
func (c *Client) Fetch(ctx context.Context, localVersion int) (*ConfigResponse, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	q := u.Query()
	q.Set("node_id", c.nodeID)
	q.Set("agent_version", c.agentVersion)
	if localVersion > 0 {
		q.Set("config_version", fmt.Sprintf("%d", localVersion))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	// TODO: Node signature / mTLS when node identity is implemented.
	// For MVP we rely on internal network + X-Node-ID.
	req.Header.Set("X-Node-ID", c.nodeID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var configResp ConfigResponse
	if err := json.Unmarshal(body, &configResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &configResp, nil
}
