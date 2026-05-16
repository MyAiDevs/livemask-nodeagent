package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegister_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/agent/register" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Node-ID") != "test-node" {
			t.Fatal("missing X-Node-ID header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"node_id":"test-node","status":"active","message":"registered"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	resp, err := client.Register(context.Background())
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if resp.NodeID != "test-node" {
		t.Fatalf("expected node_id test-node, got %s", resp.NodeID)
	}
	if resp.Status != "active" {
		t.Fatalf("expected status active, got %s", resp.Status)
	}
}

func TestRegister_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"forbidden"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	_, err := client.Register(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 register")
	}
}

func TestRegister_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	client.httpClient.Timeout = 1
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()
	_, err := client.Register(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestHeartbeat_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/agent/heartbeat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Node-ID") != "test-node" {
			t.Fatal("missing X-Node-ID header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"accepted":true,"server_time":1712345678}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	hb := &HeartbeatRequest{
		NodeID:       "test-node",
		AgentVersion: "v1.0",
		HealthStatus: "healthy",
		SystemMetrics: SystemMetrics{
			CPUPercent:    25.0,
			MemoryPercent: 50.0,
			MemoryUsedMB:  2048,
		},
	}
	resp, err := client.Heartbeat(context.Background(), hb)
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}
	if !resp.Accepted {
		t.Fatal("expected accepted=true")
	}
	if resp.ServerTime != 1712345678 {
		t.Fatalf("unexpected server_time: %d", resp.ServerTime)
	}
}

func TestHeartbeat_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error":"internal"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	_, err := client.Heartbeat(context.Background(), &HeartbeatRequest{NodeID: "test-node"})
	if err == nil {
		t.Fatal("expected error for non-200 heartbeat")
	}
}
