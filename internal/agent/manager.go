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
	SingboxStatusDisabled = "disabled"
	SingboxStatusFailed   = "failed"
	SingboxStatusUnhealthy = "unhealthy"
	SingboxStatusStarting = "starting"
)

// Manager manages the NodeAgent lifecycle: registration, identity persistence,
// heartbeat with HMAC signature, and system metrics collection.
// TASK-NODE-001, extended with sing-box integration (TASK-NODEAGENT-SINGBOX-001).
type Manager struct {
	client         *Client
	collector      MetricsCollector
	configProvider ConfigProvider
	identityStore  *IdentityStore
	singboxProvider SingboxStatusProvider

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

	pollCtx    context.Context
	pollCancel context.CancelFunc
	pollWg     sync.WaitGroup

	statusHooks []func(AgentStatus)
}

// NewManager creates a new AgentManager.
// singboxProvider may be nil; if nil, singbox status defaults to "unknown".
func NewManager(client *Client, collector MetricsCollector, configProvider ConfigProvider,
	identityStore *IdentityStore, singboxProvider SingboxStatusProvider) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		client:          client,
		collector:       collector,
		configProvider:  configProvider,
		identityStore:   identityStore,
		singboxProvider: singboxProvider,
		pollCtx:         ctx,
		pollCancel:      cancel,
	}
}

// LoadIdentity attempts to load a previously persisted node identity.
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

// Register performs the startup registration.
func (m *Manager) Register(ctx context.Context, nodeName string) error {
	m.mu.RLock()
	id := m.identity
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

	if resp.NodeSecret != "" {
		newID := &Identity{NodeID: resp.NodeID, NodeSecret: resp.NodeSecret}
		if saveErr := m.identityStore.Save(newID); saveErr != nil {
			log.Printf("[agent] failed to persist identity: %v", saveErr)
		}
		m.identity = newID
		m.client.SetNodeIdentity(resp.NodeID, resp.NodeSecret)
		log.Printf("[agent] registered new node %s, secret persisted", resp.NodeID)
	} else if id != nil {
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

// getSingboxStatus returns the current sing-box status from the provider.
// Falls back to "unknown" if no provider is configured.
func (m *Manager) getSingboxStatus() string {
	if m.singboxProvider == nil {
		return SingboxStatusUnknown
	}
	s := m.singboxProvider.Status()
	switch s.Status {
	case "disabled":
		return SingboxStatusDisabled
	case "stopped":
		return SingboxStatusStopped
	case "starting":
		return SingboxStatusStarting
	case "running":
		return SingboxStatusRunning
	case "unhealthy":
		return SingboxStatusUnhealthy
	case "failed":
		return SingboxStatusFailed
	default:
		return SingboxStatusUnknown
	}
}

// getSingboxRuntimeStatus returns the full sing-box runtime snapshot.
func (m *Manager) getSingboxRuntimeStatus() *SingboxRuntimeStatus {
	if m.singboxProvider == nil {
		return nil
	}
	s := m.singboxProvider.Status()
	return &SingboxRuntimeStatus{
		Enabled:           s.Enabled,
		Status:            s.Status,
		PID:               s.PID,
		ConfigPath:        s.ConfigPath,
		ListenHost:        s.ListenHost,
		ListenPort:        s.ListenPort,
		LastStartedAt:     s.LastStartedAt,
		LastStoppedAt:     s.LastStoppedAt,
		LastHealthCheckAt: s.LastHealthCheckAt,
		LastError:         s.LastError,
		RestartCount:      s.RestartCount,
	}
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

	// Build heartbeat request.
	m.mu.RLock()
	cfgVersion := m.configProvider.ConfigVersion()
	cfgHash := m.configProvider.ConfigHash()
	isDegraded := m.configProvider.IsDegraded()
	degradedReason := ""
	if isDegraded {
		degradedReason = "config subsystem degraded"
	}
	m.mu.RUnlock()

	sbStatus := m.getSingboxStatus()

	// Determine degraded state: combine config degraded + sing-box unhealthy/failed.
	if sbStatus == SingboxStatusFailed || sbStatus == SingboxStatusUnhealthy {
		isDegraded = true
		if degradedReason == "" {
			degradedReason = fmt.Sprintf("singbox_%s", sbStatus)
		} else {
			degradedReason = degradedReason + fmt.Sprintf("; singbox_%s", sbStatus)
		}
	}

	if metrics == nil {
		metrics = &SystemMetrics{}
	}

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
		SingboxStatus:    m.getSingboxStatus(),
		Singbox:          m.getSingboxRuntimeStatus(),
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

	// Merge degraded reasons.
	reasons := ""
	if m.configProvider.IsDegraded() {
		reasons = "config subsystem degraded"
	}
	if s.SingboxStatus == SingboxStatusFailed || s.SingboxStatus == SingboxStatusUnhealthy {
		if reasons != "" {
			reasons += "; "
		}
		reasons += fmt.Sprintf("singbox_%s", s.SingboxStatus)
	}
	if reasons != "" {
		s.HealthStatus = "degraded"
		s.Degraded = true
		s.DegradedReason = reasons
	}
	if !m.lastHeartbeatOK && m.heartbeatsSent > 0 {
		s.HealthStatus = "degraded"
		if s.DegradedReason == "" {
			s.DegradedReason = "heartbeat failed"
		}
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
