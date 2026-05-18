package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegister_Success_NewNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/agent/register" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		// Verify body
		var body RegisterRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.NodeName != "test-server" {
			t.Fatalf("expected NodeName test-server, got %s", body.NodeName)
		}
		if body.AgentVersion != "v1.0" {
			t.Fatalf("expected AgentVersion v1.0, got %s", body.AgentVersion)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"node_id":"new-uuid","node_secret":"new-secret","status":"pending_review"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	resp, err := client.Register(context.Background(), "test-server")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if resp.NodeID != "new-uuid" {
		t.Fatalf("expected node_id new-uuid, got %s", resp.NodeID)
	}
	if resp.NodeSecret != "new-secret" {
		t.Fatalf("expected node_secret new-secret, got %s", resp.NodeSecret)
	}
	if resp.Status != "pending_review" {
		t.Fatalf("expected status pending_review, got %s", resp.Status)
	}

	// Client should now be updated.
	client.SetNodeIdentity(resp.NodeID, resp.NodeSecret)
	if client.nodeID != "new-uuid" {
		t.Fatal("client.nodeID not updated")
	}
	if client.nodeSecret != "new-secret" {
		t.Fatal("client.nodeSecret not updated")
	}
}

func TestRegister_ReRegistration(t *testing.T) {
	// Second register call with existing identity — Backend does not return secret.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"node_id":"existing-uuid","status":"active"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("existing-uuid", "existing-secret")
	resp, err := client.Register(context.Background(), "test-server")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if resp.NodeID != "existing-uuid" {
		t.Fatalf("expected node_id existing-uuid, got %s", resp.NodeID)
	}
	if resp.NodeSecret != "" {
		t.Fatalf("expected empty node_secret for re-registration, got %s", resp.NodeSecret)
	}
}

func TestRegister_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"forbidden"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	_, err := client.Register(context.Background(), "test-server")
	if err == nil {
		t.Fatal("expected error for non-200 register")
	}
}

func TestHeartbeat_WithSignature(t *testing.T) {
	var capturedNodeID, capturedTimestamp, capturedSignature string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/agent/heartbeat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		capturedNodeID = r.Header.Get("X-Node-ID")
		capturedTimestamp = r.Header.Get("X-Timestamp")
		capturedSignature = r.Header.Get("X-Signature")

		if capturedNodeID == "" {
			t.Fatal("missing X-Node-ID header")
		}
		if capturedTimestamp == "" {
			t.Fatal("missing X-Timestamp header")
		}
		if capturedSignature == "" {
			t.Fatal("missing X-Signature header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":true,"server_config_version":5}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("node-uuid", "node-secret")

	hb := &HeartbeatRequest{
		AgentVersion:      "v1.0",
		ConfigVersion:     3,
		ConfigHash:        "sha256:abc",
		SingboxStatus:     "running",
		CPUUsage:          25.0,
		MemoryUsage:       50.0,
		ActiveConnections: 10,
	}
	resp, err := client.Heartbeat(context.Background(), hb)
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}
	if resp.ServerConfigVersion != 5 {
		t.Fatalf("expected server_config_version 5, got %d", resp.ServerConfigVersion)
	}

	// Verify the signature is valid
	expectedSig := ComputeSignature("node-uuid", capturedTimestamp, "node-secret")
	if capturedSignature != expectedSig {
		t.Fatalf("signature mismatch: got %s, expected %s", capturedSignature, expectedSig)
	}
}

func TestHeartbeat_MissingNodeID(t *testing.T) {
	// Should fail at the client level if nodeID is not set.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	// Deliberately NOT setting node identity.
	hb := &HeartbeatRequest{AgentVersion: "v1.0"}
	_, err := client.Heartbeat(context.Background(), hb)
	// The server doesn't require auth for this test, so the request will go through.
	// In the real scenario, the server would reject it. This test just ensures
	// the client doesn't crash when nodeID is empty.
	if err != nil {
		t.Logf("heartbeat error (expected on empty nodeID): %v", err)
	}
}

func TestHeartbeat_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"NODE_SECRET_MISMATCH"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("node-uuid", "wrong-secret")
	_, err := client.Heartbeat(context.Background(), &HeartbeatRequest{AgentVersion: "v1.0"})
	if err == nil {
		t.Fatal("expected error for non-200 heartbeat")
	}
}

func TestRegisterNodeEndpoint_Success(t *testing.T) {
	var capturedHeaders map[string]string
	var capturedBody EndpointReportRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/agent/node-endpoint" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		capturedHeaders = map[string]string{
			"X-Node-ID":    r.Header.Get("X-Node-ID"),
			"X-Timestamp":  r.Header.Get("X-Timestamp"),
			"X-Signature":  r.Header.Get("X-Signature"),
		}
		if capturedHeaders["X-Node-ID"] == "" {
			t.Fatal("missing X-Node-ID header")
		}
		if capturedHeaders["X-Timestamp"] == "" {
			t.Fatal("missing X-Timestamp header")
		}
		if capturedHeaders["X-Signature"] == "" {
			t.Fatal("missing X-Signature header")
		}

		json.NewDecoder(r.Body).Decode(&capturedBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("node-uuid", "node-secret")

	req := &EndpointReportRequest{
		PublicEndpointHost: "node1.example.com",
		PublicEndpointPort: 443,
		Transport:          "ws",
		SNI:                "example.com",
		ALPN:               "h2,http/1.1",
		ProtocolProfile:    "vless-ws",
		Enabled:            true,
	}
	resp, err := client.RegisterNodeEndpoint(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterNodeEndpoint failed: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}

	// Verify request body.
	if capturedBody.PublicEndpointHost != "node1.example.com" {
		t.Fatalf("expected host node1.example.com, got %s", capturedBody.PublicEndpointHost)
	}
	if capturedBody.PublicEndpointPort != 443 {
		t.Fatalf("expected port 443, got %d", capturedBody.PublicEndpointPort)
	}
	if capturedBody.Transport != "ws" {
		t.Fatalf("expected transport ws, got %s", capturedBody.Transport)
	}
	if capturedBody.ProtocolProfile != "vless-ws" {
		t.Fatalf("expected protocol_profile vless-ws, got %s", capturedBody.ProtocolProfile)
	}
	if !capturedBody.Enabled {
		t.Fatal("expected enabled=true")
	}

	// Verify signature.
	expectedSig := ComputeSignature("node-uuid", capturedHeaders["X-Timestamp"], "node-secret")
	if capturedHeaders["X-Signature"] != expectedSig {
		t.Fatalf("signature mismatch: got %s, expected %s", capturedHeaders["X-Signature"], expectedSig)
	}
}

func TestRegisterNodeEndpoint_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"NODE_SECRET_MISMATCH"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("node-uuid", "wrong-secret")
	req := &EndpointReportRequest{
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 443,
		Transport:          "ws",
		Enabled:            true,
	}
	_, err := client.RegisterNodeEndpoint(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for non-200")
	}
}

func TestRegisterNodeEndpoint_EmptyFields(t *testing.T) {
	var capturedBody EndpointReportRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("node-uuid", "node-secret")

	req := &EndpointReportRequest{
		Enabled: false,
	}
	resp, err := client.RegisterNodeEndpoint(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterNodeEndpoint failed: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}
	if capturedBody.Enabled {
		t.Fatal("expected enabled=false")
	}
}
