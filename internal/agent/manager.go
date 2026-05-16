package agent

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

const (
	// DefaultHeartbeatInterval is the default interval between heartbeats.
	DefaultHeartbeatInterval = 30 * time.Second

	// SingBox status constants matching Backend singbox_status enum.
	SingboxStatusUnknown  = "unknown"
	SingboxStatusRunning  = "running"
	SingboxStatusDegraded = "degraded"
	SingboxStatusStopped  = "stopped"
)

// Manager manages the NodeAgent lifecycle: registration, identity persistence,
// heartbeat with HMAC signature, and system metrics collection.
// Aligned to Backend commit 02794f0. TASK-NODE-001.
type Manager struct {
	client         *Client
	collector      MetricsCollector
	configProvider ConfigProvider
	identityStore  *IdentityStore

	mu              sync.RWMutex
	identity        *Identity
	registered      bool
	registerAt      *time.Time
	registerErr     string
	heartbeatsSent  int64
	lastHeartbeatAt *time.Time
	lastHeartbeatOK bool
	lastHeartbeatErr string
	lastMetrics     *SystemMetrics
	singboxStatus   string

	pollCtx    context.Context
	pollCancel context.CancelFunc
	pollWg     sync.WaitGroup

	statusHooks []func(AgentStatus)
}

// NewManager creates a new AgentManager.
func NewManager(client *Client, collector MetricsCollector, configProvider ConfigProvider, identityStore *IdentityStore) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		client:         client,
		collector:      collector,
		configProvider: configProvider,
		identityStore:  identityStore,
		pollCtx:        ctx,
		pollCancel:     cancel,
		singboxStatus:  SingboxStatusUnknown,
	}
}

// SetSingboxStatus allows the sing-box controller to update the sing-box
// health status used in heartbeats.
func (m *Manager) SetSingboxStatus(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.singboxStatus = status
}

// LoadIdentity attempts to load a previously persisted node identity. If
// successful, the client is configured with the credentials and Register()
// can be skipped on subsequent starts. Returns true if identity was loaded.
func (m *Manager) LoadIdentity() bool {
	id, err := m.identityStore.Load()
	if err != nil {
		log.Printf("[agent] failed to load identity: %v", err)
		return false
	}
	if id == nil || id.NodeID == "" || id.NodeSecret == "" {
		return false
	}

	m.mu.Lock()
	m.identity = id
	m.registered = true
	m.mu.Unlock()

	m.client.SetNodeIdentity(id.NodeID, id.NodeSecret)
	log.Printf("[agent] loaded identity for node %s from %s", id.NodeID, m.identityStore.FilePath())
	return true
}

// Register performs the startup registration. On success the returned
// node_id and node_secret are persisted to disk and set on the client.
// Registration is REQUIRED only on first start; subsequent starts use
// LoadIdentity instead. If register fails, the agent continues in
// degraded mode and does NOT exit.
func (m *Manager) Register(ctx context.Context, nodeName string) error {
	// If we already have a local identity, inject it into the request.
	m.mu.RLock()
	id := m.identity // may be nil on first start
	m.mu.RUnlock()

	if id != nil {
		m.client.SetNodeIdentity(id.NodeID, id.NodeSecret)
	}

	resp, err := m.client.Register(ctx, nodeName)
	if err != nil {
		m.mu.Lock()
		m.registerErr = err.Error()
		m.mu.Unlock()
		m.fireStatusHooks()
		return fmt.Errorf("register failed: %w", err)
	}

	now := time.Now()
	m.mu.Lock()
	m.registered = true
	m.registerAt = &now
	m.registerErr = ""

	// If the Backend returned a node_secret (first registration), persist it.
	if resp.NodeSecret != "" {
		newID := &Identity{NodeID: resp.NodeID, NodeSecret: resp.NodeSecret}
		if saveErr := m.identityStore.Save(newID); saveErr != nil {
			log.Printf("[agent] failed to persist identity: %v", saveErr)
		}
		m.identity = newID
		m.client.SetNodeIdentity(resp.NodeID, resp.NodeSecret)
		log.Printf("[agent] registered new node %s, secret persisted", resp.NodeID)
	} else if id != nil {
		// Re-registration — Backend did not return a new secret, keep existing.
		log.Printf("[agent] re-registered node %s (status=%s)", resp.NodeID, resp.Status)
	}
	m.mu.Unlock()

	m.fireStatusHooks()
	return nil
}

// StartHeartbeatLoop starts the background heartbeat goroutine.
func (m *Manager) StartHeartbeatLoop(interval time.Duration) {
	m.pollWg.Add(1)
	go func() {
		defer m.pollWg.Done()
		currentInterval := interval
		for {
			select {
			case <-m.pollCtx.Done():
				return
			case <-time.After(jitteredInterval(currentInterval)):
				err := m.sendHeartbeat()
				if err != nil {
					log.Printf("[agent] heartbeat error: %v", err)
					currentInterval = backoff(currentInterval, interval, 2.0, 10*time.Minute)
				} else {
					currentInterval = interval
				}
			}
		}
	}()
}

// StopHeartbeatLoop stops the background heartbeat goroutine.
func (m *Manager) StopHeartbeatLoop() {
	m.pollCancel()
	m.pollWg.Wait()
}

// sendHeartbeat collects metrics, builds the request, and posts to Backend.
func (m *Manager) sendHeartbeat() error {
	// Collect system metrics.
	metrics, err := m.collector.Collect()
	if err != nil {
		log.Printf("[agent] metrics collection partial: %v", err)
	}
	if metrics != nil {
		m.mu.Lock()
		m.lastMetrics = metrics
		m.mu.Unlock()
	}

	// Build heartbeat request matching Backend HeartbeatRequest fields.
	m.mu.RLock()
	cfgVersion := m.configProvider.ConfigVersion()
	cfgHash := m.configProvider.ConfigHash()
	isDegraded := m.configProvider.IsDegraded()
	degradedReason := ""
	if isDegraded {
		degradedReason = "config subsystem degraded"
	}

	sbStatus := m.singboxStatus
	m.mu.RUnlock()

	if metrics == nil {
		metrics = &SystemMetrics{}
	}

	// Map SystemMetrics → Backend HeartbeatRequest fields.
	// load_score is a coarse proxy from load_1.
	loadScore := int(metrics.Load1 + 0.5)
	if loadScore > 100 {
		loadScore = 100
	}

	hb := &HeartbeatRequest{
		AgentVersion:      m.client.agentVersion,
		ConfigVersion:     cfgVersion,
		ConfigHash:        cfgHash,
		SingboxStatus:     sbStatus,
		LoadScore:         loadScore,
		CPUUsage:          metrics.CPUPercent,
		MemoryUsage:       metrics.MemoryPercent,
		ActiveConnections: metrics.ActiveConnections,
		Degraded:          isDegraded,
		DegradedReason:    degradedReason,
	}

	ctx, cancel := context.WithTimeout(m.pollCtx, 15*time.Second)
	defer cancel()

	hbResp, err := m.client.Heartbeat(ctx, hb)
	now := time.Now()

	m.mu.Lock()
	m.heartbeatsSent++
	m.lastHeartbeatAt = &now

	if err != nil {
		m.lastHeartbeatOK = false
		m.lastHeartbeatErr = err.Error()
		m.mu.Unlock()
		m.fireStatusHooks()
		return fmt.Errorf("send heartbeat: %w", err)
	}

	m.lastHeartbeatOK = true
	m.lastHeartbeatErr = ""
	m.mu.Unlock()

	log.Printf("[agent] heartbeat sent (cfg v%d, degraded=%v, sb=%s, ok=%v, server_cfg_v=%d)",
		cfgVersion, isDegraded, sbStatus, hbResp.OK, hbResp.ServerConfigVersion)
	m.fireStatusHooks()

	return nil
}

// Status returns an observable snapshot of agent state.
func (m *Manager) Status() AgentStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := AgentStatus{
		IsDeployed:       true,
		Registered:       m.registered,
		NodeID:           "",
		NodeStatus:       "",
		HeartbeatsSent:   m.heartbeatsSent,
		LastHeartbeatOK:  m.lastHeartbeatOK,
		LastHeartbeatErr: m.lastHeartbeatErr,
		HealthStatus:     "healthy",
		Degraded:         m.configProvider.IsDegraded(),
		SingboxStatus:    m.singboxStatus,
		LastSystemMetrics: m.lastMetrics,
		IdentityFile:     m.identityStore.FilePath(),
	}
	if m.identity != nil {
		s.NodeID = m.identity.NodeID
	}
	if m.registerAt != nil {
		unix := m.registerAt.Unix()
		s.LastRegisterAt = &unix
	}
	s.LastRegisterErr = m.registerErr
	if m.lastHeartbeatAt != nil {
		unix := m.lastHeartbeatAt.Unix()
		s.LastHeartbeatAt = &unix
	}
	if m.configProvider.IsDegraded() {
		s.HealthStatus = "degraded"
		s.DegradedReason = "config subsystem degraded"
	}
	if !m.lastHeartbeatOK && m.heartbeatsSent > 0 {
		s.HealthStatus = "degraded"
	}
	return s
}

// OnStatusChange registers a callback that fires when the agent status changes.
func (m *Manager) OnStatusChange(hook func(AgentStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusHooks = append(m.statusHooks, hook)
}

func (m *Manager) fireStatusHooks() {
	status := m.Status()
	m.mu.RLock()
	hooks := make([]func(AgentStatus), len(m.statusHooks))
	copy(hooks, m.statusHooks)
	m.mu.RUnlock()
	for _, h := range hooks {
		h(status)
	}
}

// ---- shared helpers ----

const maxJitterFraction = 0.25

func jitteredInterval(base time.Duration) time.Duration {
	jitter := time.Duration(float64(base) * maxJitterFraction * rand.Float64())
	return base + jitter
}

func backoff(current, base time.Duration, multiplier float64, max time.Duration) time.Duration {
	next := time.Duration(float64(current) * multiplier)
	if next > max {
		next = max
	}
	if next < base {
		next = base
	}
	return next
}
