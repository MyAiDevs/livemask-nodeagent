package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client communicates with Backend for agent register and heartbeat.
// TASK-NODE-001 — aligned to Backend commit 02794f0.
type Client struct {
	httpClient   *http.Client
	backendURL   string
	nodeID       string
	nodeSecret   string
	agentVersion string
}

// NewClient creates a new agent Client.
func NewClient(backendBaseURL, agentVersion string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		backendURL:   backendBaseURL,
		agentVersion: agentVersion,
	}
}

// SetNodeIdentity updates the node credentials used for heartbeat signing.
func (c *Client) SetNodeIdentity(nodeID, nodeSecret string) {
	c.nodeID = nodeID
	c.nodeSecret = nodeSecret
}

// Register sends POST /internal/agent/register (no auth required).
// The Backend generates node_id + node_secret if not provided.
func (c *Client) Register(ctx context.Context, nodeName string) (*RegisterResponse, error) {
	u, err := url.JoinPath(c.backendURL, "/internal/agent/register")
	if err != nil {
		return nil, fmt.Errorf("build register url: %w", err)
	}

	body := RegisterRequest{
		NodeID:       c.nodeID,
		NodeSecret:   c.nodeSecret,
		NodeName:     nodeName,
		AgentVersion: c.agentVersion,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal register request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read register response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("register unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var registerResp RegisterResponse
	if err := json.Unmarshal(respBody, &registerResp); err != nil {
		return nil, fmt.Errorf("unmarshal register response: %w", err)
	}
	return &registerResp, nil
}

// Heartbeat sends POST /internal/agent/heartbeat with HMAC auth headers:
// X-Node-ID, X-Timestamp, X-Signature.
func (c *Client) Heartbeat(ctx context.Context, hb *HeartbeatRequest) (*HeartbeatResponse, error) {
	u, err := url.JoinPath(c.backendURL, "/internal/agent/heartbeat")
	if err != nil {
		return nil, fmt.Errorf("build heartbeat url: %w", err)
	}

	payload, err := json.Marshal(hb)
	if err != nil {
		return nil, fmt.Errorf("marshal heartbeat: %w", err)
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create heartbeat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Node-ID", c.nodeID)
	req.Header.Set("X-Timestamp", timestamp)

	// Signature: HEX(HMAC-SHA256(key=node_secret, msg=nodeID:timestamp))
	sig := ComputeSignature(c.nodeID, timestamp, c.nodeSecret)
	req.Header.Set("X-Signature", sig)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("heartbeat http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read heartbeat response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("heartbeat unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var hbResp HeartbeatResponse
	if err := json.Unmarshal(respBody, &hbResp); err != nil {
		return nil, fmt.Errorf("unmarshal heartbeat response: %w", err)
	}
	return &hbResp, nil
}
