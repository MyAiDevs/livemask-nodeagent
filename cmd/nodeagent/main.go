// TASK-NODE-001 — NodeAgent registration, heartbeat, HMAC auth, and system metrics.
//
// This binary wires up both the config subsystem (TASK-NA-CONFIG-001) and
// the agent registration/heartbeat subsystem (TASK-NODE-001).
//
// Identity lifecycle:
//   1. First start → POST /internal/agent/register → save node_id+node_secret
//   2. Subsequent starts → load identity from IDENTITY_PATH, skip register
//   3. Heartbeat uses HMAC-SHA256(node_id:timestamp, key=node_secret)
//
// Usage:
//
//	BACKEND_BASE_URL=http://backend:8080 \
//	AGENT_VERSION=v0.1.0 \
//	NODE_NAME=my-server-1 \
//	IDENTITY_PATH=/var/lib/nodeagent/identity.json \
//	CONFIG_CACHE_PATH=/var/lib/nodeagent/config-cache.json \
//	POLL_INTERVAL_SEC=60 \
//	HEARTBEAT_INTERVAL_SEC=30 \
//	LISTEN_ADDR=:9100 \
//	  ./nodeagent
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MyAiDevs/livemask-nodeagent/internal/agent"
	"github.com/MyAiDevs/livemask-nodeagent/internal/config"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("[main] NodeAgent starting — TASK-NODE-001")

	// ---- Environment configuration ----
	backendBaseURL := mustEnv("BACKEND_BASE_URL")
	agentVersion := mustEnv("AGENT_VERSION")
	nodeName := envOrDefault("NODE_NAME", "nodeagent")
	identityPath := envOrDefault("IDENTITY_PATH", "identity.json")
	cachePath := envOrDefault("CONFIG_CACHE_PATH", "config-cache.json")
	listenAddr := envOrDefault("LISTEN_ADDR", ":9100")
	pollIntervalSec := envIntOrDefault("POLL_INTERVAL_SEC", 60)
	heartbeatIntervalSec := envIntOrDefault("HEARTBEAT_INTERVAL_SEC", 30)
	pollInterval := time.Duration(pollIntervalSec) * time.Second
	heartbeatInterval := time.Duration(heartbeatIntervalSec) * time.Second

	backendBaseURL = strings.TrimRight(backendBaseURL, "/")

	// ---- Build the config subsystem (same as TASK-NA-CONFIG-001) ----
	configURL := backendBaseURL + "/internal/agent/config"
	cfgClient := config.NewClient(configURL, "", agentVersion)
	cfgStore := config.NewStore(cachePath)

	applier := config.NewRuntimeApplier(func(old, new *config.RuntimeConfig) error {
		log.Printf("[config] **** Applying config change ****")
		log.Printf("[config]   heartbeat_interval:        %d -> %d",
			old.Reporting.HeartbeatIntervalSeconds, new.Reporting.HeartbeatIntervalSeconds)
		log.Printf("[config]   batch_upload_interval:      %d -> %d",
			old.Reporting.BatchUploadIntervalSeconds, new.Reporting.BatchUploadIntervalSeconds)
		log.Printf("[config]   max_offline_buffer_items:   %d -> %d",
			old.Reporting.MaxOfflineBufferItems, new.Reporting.MaxOfflineBufferItems)
		log.Printf("[config]   degraded_mode.enabled:      %t -> %t",
			old.DegradedMode.Enabled, new.DegradedMode.Enabled)
		log.Printf("[config]   degraded_mode.auto_recover: %t -> %t",
			old.DegradedMode.AutoRecover, new.DegradedMode.AutoRecover)
		log.Printf("[config]   health_check_timeout:       %d -> %d",
			old.Singbox.HealthCheckTimeoutSeconds, new.Singbox.HealthCheckTimeoutSeconds)
		log.Printf("[config] **** Config applied successfully ****")
		return nil
	})
	cfgMgr := config.NewManager(cfgClient, cfgStore, applier)

	// ---- Build the agent subsystem ----
	agentClient := agent.NewClient(backendBaseURL, agentVersion)
	sysCollector := agent.NewSystemCollector()
	identityStore := agent.NewIdentityStore(identityPath)

	// Agent Manager — cfgMgr implements ConfigProvider.
	agentMgr := agent.NewManager(agentClient, sysCollector, cfgMgr, identityStore)

	// ---- Identity lifecycle ----
	// 1. Attempt to load persisted identity (node_id + node_secret).
	hadIdentity := agentMgr.LoadIdentity()

	// 2. If no identity yet, register for the first time.
	if !hadIdentity {
		log.Println("[main] No persisted identity — registering with Backend")
		registerCtx, registerCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if regErr := agentMgr.Register(registerCtx, nodeName); regErr != nil {
			log.Printf("[main] Registration failed (continuing in degraded mode): %v", regErr)
		} else {
			log.Printf("[main] Registration successful, node_id=%s", agentMgr.Status().NodeID)
		}
		registerCancel()
	} else {
		log.Printf("[main] Identity loaded — skipping registration")
	}

	// ---- Startup: load last-known-good config, then sync ----
	loaded := cfgMgr.LoadLastKnownGood()
	if loaded {
		log.Printf("[main] Bootstrapped from last-known-good (version %d)",
			cfgMgr.Status().ConfigVersion)
	} else {
		log.Println("[main] No last-known-good cache — starting fresh")
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 15*time.Second)
	changed, err := cfgMgr.SyncOnce(initCtx)
	initCancel()
	if err != nil {
		log.Printf("[main] Initial config sync failed (degraded mode): %v", err)
	} else if changed {
		log.Printf("[main] Initial config synced to version %d", cfgMgr.Status().ConfigVersion)
	} else {
		log.Printf("[main] Config is current (version %d)", cfgMgr.Status().ConfigVersion)
	}

	// ---- Background polling (config + heartbeat) ----
	cfgMgr.StartPoll(pollInterval)
	log.Printf("[main] Config polling started (interval=%v)", pollInterval)

	if hadIdentity || agentMgr.Status().Registered {
		agentMgr.StartHeartbeatLoop(heartbeatInterval)
		log.Printf("[main] Heartbeat loop started (interval=%v)", heartbeatInterval)
	} else {
		log.Println("[main] Heartbeat loop NOT started (no node identity)")
	}

	// ---- HTTP status endpoints ----
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := "ok"
		agentStatus := agentMgr.Status()
		if cfgMgr.Status().IsDegraded || (agentStatus.HeartbeatsSent > 0 && !agentStatus.LastHeartbeatOK) {
			status = "degraded"
		}
		if !hadIdentity && !agentStatus.Registered {
			status = "degraded"
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":%q,"timestamp":%d}`, status, time.Now().Unix())
	})
	mux.HandleFunc("/config/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(cfgMgr.Status())
	})
	mux.HandleFunc("/config/reload", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		changed, err := cfgMgr.SyncOnce(ctx)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, `{"error":%q}`, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"changed":%v,"version":%d}`, changed, cfgMgr.Status().ConfigVersion)
	})
	mux.HandleFunc("/agent/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(agentMgr.Status())
	})

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("[main] received signal %v — shutting down", sig)

		agentMgr.StopHeartbeatLoop()
		cfgMgr.StopPoll()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("[main] HTTP server listening on %s", listenAddr)
	if serveErr := server.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
		log.Fatalf("[main] HTTP server error: %v", serveErr)
	}

	log.Println("[main] NodeAgent stopped")
}

// ---- helpers ----

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[main] required env %q is not set", key)
	}
	return v
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
		log.Printf("[main] invalid int for %q=%q, using default %d", key, v, def)
	}
	return def
}
