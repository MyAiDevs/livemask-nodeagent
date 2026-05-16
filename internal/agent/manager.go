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

	// DefaultRegisterTimeout is how long to wait for the initial registration.
	DefaultRegisterTimeout = 15 * time.Second

	// status constants.
	healthHealthy  = "healthy"
	healthDegraded = "degraded"
	healthDown     = "down"

	singboxHealthy   = "healthy"
	singboxUnhealthy = "unhealthy"
	singboxUnknown   = "unknown"
)

// Manager manages the NodeAgent lifecycle: registration, heartbeat, and
// system metrics collection. TASK-NODE-001.
type Manager struct {
	client         *Client
	collector      MetricsCollector
	configProvider ConfigProvider

	mu               sync.RWMutex
	registered       bool
	registerResp     *RegisterResponse
	registerAt       *time.Time
	registerErr      string
	heartbeatsSent   int64
	lastHeartbeatAt  *time.Time
	lastHeartbeatOK  bool
	lastHeartbeatErr string
	lastMetrics      *SystemMetrics
	healthStatus     string
	singboxStatus    string
	healthStatusMu   sync.RWMutex

	pollCtx    context.Context
	pollCancel context.CancelFunc
	pollWg     sync.WaitGroup

	statusHooks []func(AgentStatus)
}

// NewManager creates a new AgentManager.
func NewManager(client *Client, collector MetricsCollector, configProvider ConfigProvider) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		client:         client,
		collector:      collector,
		configProvider: configProvider,
		pollCtx:        ctx,
		pollCancel:     cancel,
		healthStatus:   healthHealthy,
		singboxStatus:  singboxUnknown,
	}
	return m
}

// SetSingboxStatus allows the sing-box controller to update the sing-box
// health status used in heartbeats.
func (m *Manager) SetSingboxStatus(status string) {
	m.healthStatusMu.Lock()
	defer m.healthStatusMu.Unlock()
	m.singboxStatus = status
}

// Register performs the startup registration. It is meant to be called once
// during boot. If it fails, the agent logs a warning and enters degraded mode
// but does NOT exit the process.
func (m *Manager) Register(ctx context.Context) error {
	resp, err := m.client.Register(ctx)
	if err != nil {
		m.mu.Lock()
		m.registerErr = err.Error()
		m.healthStatus = healthDegraded
		m.mu.Unlock()
		m.fireStatusHooks()
		return fmt.Errorf("register failed: %w", err)
	}

	now := time.Now()
	m.mu.Lock()
	m.registered = true
	m.registerResp = resp
	m.registerAt = &now
	m.registerErr = ""
	// Don't mark as degraded just because status is pending_review.
	m.mu.Unlock()

	log.Printf("[agent] registered node %s with status %q", resp.NodeID, resp.Status)
	m.fireStatusHooks()
	return nil
}

// StartHeartbeatLoop starts the background heartbeat goroutine. Call
// StopHeartbeatLoop to stop it.
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

// StopHeartbeatLoop gracefully stops the background heartbeat goroutine.
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

	// Build heartbeat request.
	m.mu.RLock()
	cfgVersion := m.configProvider.ConfigVersion()
	cfgHash := m.configProvider.ConfigHash()
	isDegraded := m.configProvider.IsDegraded()
	degradedReason := ""
	if isDegraded {
		m.healthStatus = healthDegraded
		degradedReason = "config subsystem degraded"
	}

	// Determine health status based on various factors.
	healthStatus := m.healthStatus
	if healthStatus == "" {
		healthStatus = healthHealthy
	}

	m.mu.RUnlock()

	m.healthStatusMu.RLock()
	sbStatus := m.singboxStatus
	m.healthStatusMu.RUnlock()

	if metrics == nil {
		metrics = &SystemMetrics{}
	}

	hb := &HeartbeatRequest{
		NodeID:         m.client.nodeID,
		AgentVersion:   m.client.agentVersion,
		ConfigVersion:  cfgVersion,
		ConfigHash:     cfgHash,
		HealthStatus:   healthStatus,
		Degraded:       isDegraded,
		DegradedReason: degradedReason,
		SingboxStatus:  sbStatus,
		SystemMetrics:  *metrics,
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
		m.healthStatus = healthDegraded
		m.mu.Unlock()
		m.fireStatusHooks()
		return fmt.Errorf("send heartbeat: %w", err)
	}

	m.lastHeartbeatOK = true
	m.lastHeartbeatErr = ""
	// Reset health to healthy only if it was previously down/degraded from
	// heartbeat failures and now succeeds.
	if m.healthStatus == healthDegraded && !isDegraded {
		// Only restore to healthy if the config manager is not degraded.
		m.healthStatus = healthHealthy
	}
	m.mu.Unlock()

	log.Printf("[agent] heartbeat sent (cfg v%d, degraded=%v, sb=%s, accepted=%v)",
		cfgVersion, isDegraded, sbStatus, hbResp.Accepted)
	m.fireStatusHooks()

	return nil
}

// Status returns an observable snapshot of agent registration and heartbeat
// state.
func (m *Manager) Status() AgentStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := AgentStatus{
		IsDeployed:       true,
		Registered:       m.registered,
		NodeStatus:       "",
		HeartbeatsSent:   m.heartbeatsSent,
		LastHeartbeatOK:  m.lastHeartbeatOK,
		LastHeartbeatErr: m.lastHeartbeatErr,
		HealthStatus:     m.healthStatus,
		Degraded:         m.configProvider.IsDegraded(),
		DegradedReason:   "",
		SingboxStatus:    m.singboxStatus,
		LastSystemMetrics: m.lastMetrics,
	}
	if m.registerResp != nil {
		s.NodeStatus = m.registerResp.Status
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
		s.DegradedReason = "config subsystem degraded"
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

// ---- shared helpers (also used by config package) ----

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
