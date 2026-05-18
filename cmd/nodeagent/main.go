// TASK-NODEAGENT-SINGBOX-001 — NodeAgent sing-box runtime management and health status.
//
// This binary wires up:
//   - Config sync (TASK-NA-CONFIG-001)
//   - Agent registration/heartbeat (TASK-NODE-001)
//   - sing-box runtime lifecycle (TASK-NODEAGENT-SINGBOX-001)
//
// Usage:
//
//	BACKEND_BASE_URL=http://backend:8080 \
//	AGENT_VERSION=v0.2.0 \
//	NODE_NAME=my-server-1 \
//	IDENTITY_PATH=/var/lib/nodeagent/identity.json \
//	CONFIG_CACHE_PATH=/var/lib/nodeagent/config-cache.json \
//	SINGBOX_ENABLED=false \
//	SINGBOX_BIN_PATH=sing-box \
//	SINGBOX_CONFIG_PATH=/var/lib/nodeagent/singbox.json \
//	SINGBOX_WORK_DIR=/var/lib/nodeagent \
//	SINGBOX_LOG_PATH=/var/log/nodeagent/singbox.log \
//	SINGBOX_LISTEN_HOST=127.0.0.1 \
//	SINGBOX_LISTEN_PORT=10808 \
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
	"github.com/MyAiDevs/livemask-nodeagent/internal/singbox"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("[main] NodeAgent starting — TASK-NODEAGENT-SINGBOX-001")

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

	// Sing-box env configuration.
	singboxEnabled := envBoolOrDefault("SINGBOX_ENABLED", false)
	singboxBinPath := envOrDefault("SINGBOX_BIN_PATH", "sing-box")
	singboxConfigPath := envOrDefault("SINGBOX_CONFIG_PATH", "singbox.json")
	singboxWorkDir := envOrDefault("SINGBOX_WORK_DIR", ".")
	singboxLogPath := envOrDefault("SINGBOX_LOG_PATH", "singbox.log")
	singboxListenHost := envOrDefault("SINGBOX_LISTEN_HOST", "127.0.0.1")
	singboxListenPort := envIntOrDefault("SINGBOX_LISTEN_PORT", 10808)
	singboxRestartOnConfig := envBoolOrDefault("SINGBOX_RESTART_ON_CONFIG_CHANGE", true)

	backendBaseURL = strings.TrimRight(backendBaseURL, "/")

	// ---- Build the sing-box runtime manager ----
	singboxCfg := &singbox.SingboxConfig{
		Enabled:               singboxEnabled,
		BinPath:               singboxBinPath,
		ConfigPath:            singboxConfigPath,
		WorkDir:               singboxWorkDir,
		LogPath:               singboxLogPath,
		ListenHost:            singboxListenHost,
		ListenPort:            singboxListenPort,
		LogLevel:              "info",
		RestartOnConfigChange: singboxRestartOnConfig,
	}
	singboxMgr := singbox.NewManager(singboxCfg)

	// ---- Build the config subsystem ----
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
		log.Printf("[config]   singbox.listen_host:        %s -> %s",
			old.Singbox.ListenHost, new.Singbox.ListenHost)
		log.Printf("[config]   singbox.listen_port:        %d -> %d",
			old.Singbox.ListenPort, new.Singbox.ListenPort)
		log.Printf("[config]   singbox.health_check:       %d -> %d",
			old.Singbox.HealthCheckTimeoutSeconds, new.Singbox.HealthCheckTimeoutSeconds)
		log.Printf("[config] **** Config applied successfully ****")

		// Propagate config center values into sing-box runtime config.
		singboxCfg.ListenHost = new.Singbox.ListenHost
		singboxCfg.ListenPort = new.Singbox.ListenPort
		singboxCfg.LogLevel = new.Singbox.LogLevel
		_ = old

		// Apply/render sing-box config if enabled.
		// Use a short timeout for config application.
		applyCtx, applyCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer applyCancel()
		if err := singboxMgr.ApplyConfig(applyCtx, singboxCfg, ""); err != nil {
			log.Printf("[config] singbox apply failed (continuing): %v", err)
			// Return error so config manager enters degraded mode.
			return fmt.Errorf("singbox apply: %w", err)
		}
		return nil
	})
	cfgMgr := config.NewManager(cfgClient, cfgStore, applier)

	// ---- Build the agent subsystem ----
	agentClient := agent.NewClient(backendBaseURL, agentVersion)
	sysCollector := agent.NewSystemCollector()
	identityStore := agent.NewIdentityStore(identityPath)

	// Agent Manager — cfgMgr implements ConfigProvider, singboxMgr implements SingboxStatusProvider.
	agentMgr := agent.NewManager(agentClient, sysCollector, cfgMgr, identityStore, singboxMgr)

	// ---- Sing-box: startup health and render on first config ----
	sboxCtx, sboxCancel := context.WithCancel(context.Background())
	defer sboxCancel()

	if singboxEnabled {
		// Render initial config from env defaults (will be updated when config syncs).
		if renderErr := singbox.Render(singboxCfg); renderErr != nil {
			log.Printf("[main] singbox initial render failed: %v", renderErr)
		}
		if startErr := singboxMgr.Start(sboxCtx); startErr != nil {
			log.Printf("[main] singbox start failed (degraded): %v", startErr)
		}
		singboxMgr.StartHealthLoop(sboxCtx)
	} else {
		log.Println("[main] Sing-box disabled — runtime manager inactive")
	}

	// ---- Identity lifecycle ----
	hadIdentity := agentMgr.LoadIdentity()
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

	// Update config client with real node_id after registration.
	agentStatus := agentMgr.Status()
	if agentStatus.NodeID != "" {
		cfgClient.SetNodeID(agentStatus.NodeID)
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
		as := agentMgr.Status()
		status := "ok"
		if cfgMgr.Status().IsDegraded || (as.HeartbeatsSent > 0 && !as.LastHeartbeatOK) {
			status = "degraded"
		}
		if singboxEnabled && (as.SingboxStatus == "failed" || as.SingboxStatus == "unhealthy") {
			status = "degraded"
		}
		if !hadIdentity && !as.Registered {
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

		// Stop subsystems in reverse order.
		agentMgr.StopHeartbeatLoop()
		cfgMgr.StopPoll()

		// Stop sing-box.
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer stopCancel()
		if err := singboxMgr.Stop(stopCtx); err != nil {
			log.Printf("[main] singbox stop error: %v", err)
		}
		sboxCancel()
		singboxMgr.WaitForShutdown()

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

func envBoolOrDefault(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		default:
			log.Printf("[main] invalid bool for %q=%q, using default %t", key, v, def)
		}
	}
	return def
}
