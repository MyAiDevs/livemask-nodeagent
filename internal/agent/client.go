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
type Client struct {
	httpClient   *http.Client
	backendURL   string
	nodeID       string
	agentVersion string
}

// NewClient creates a new agent Client.
// backendBaseURL is the base URL of the Backend, e.g. "http://backend:8080".
func NewClient(backendBaseURL, nodeID, agentVersion string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		backendURL:   backendBaseURL,
		nodeID:       nodeID,
		agentVersion: agentVersion,
	}
}

// Register sends a POST /internal/agent/register request on startup.
func (c *Client) Register(ctx context.Context) (*RegisterResponse, error) {
	u, err := url.JoinPath(c.backendURL, "/internal/agent/register")
	if err != nil {
		return nil, fmt.Errorf("build register url: %w", err)
	}

	body := RegisterRequest{
		NodeID:       c.nodeID,
		AgentVersion: c.agentVersion,
		Timestamp:    time.Now().Unix(),
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
	req.Header.Set("X-Node-ID", c.nodeID)

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

// Heartbeat sends a POST /internal/agent/heartbeat request.
func (c *Client) Heartbeat(ctx context.Context, hb *HeartbeatRequest) (*HeartbeatResponse, error) {
	u, err := url.JoinPath(c.backendURL, "/internal/agent/heartbeat")
	if err != nil {
		return nil, fmt.Errorf("build heartbeat url: %w", err)
	}

	hb.Timestamp = time.Now().Unix()
	payload, err := json.Marshal(hb)
	if err != nil {
		return nil, fmt.Errorf("marshal heartbeat: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create heartbeat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Node-ID", c.nodeID)

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
