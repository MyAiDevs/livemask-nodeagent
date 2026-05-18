package singbox

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const (
	// DefaultStopTimeout is how long to wait for graceful SIGTERM before SIGKILL.
	DefaultStopTimeout = 10 * time.Second

	// DefaultHealthCheckInterval for the background health goroutine.
	DefaultHealthCheckInterval = 10 * time.Second
)

// Manager manages the lifecycle of a local sing-box process.
// TASK-NODEAGENT-SINGBOX-001, TASK-NODEAGENT-SINGBOX-002.
type Manager struct {
	cfg        *SingboxConfig
	status     RuntimeStatus
	statusMu   sync.RWMutex
	cmd        *exec.Cmd
	cmdMu      sync.Mutex
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	configHash string
}

// NewManager creates a new sing-box Manager using the supplied config.
func NewManager(cfg *SingboxConfig) *Manager {
	return &Manager{
		cfg: cfg,
		status: RuntimeStatus{
			Enabled:            cfg.Enabled,
			Status:             string(StatusDisabled),
			ConfigPath:         cfg.ConfigPath,
			ListenHost:         cfg.ListenHost,
			ListenPort:         cfg.ListenPort,
			Transport:          cfg.Transport,
			ProtocolProfile:    cfg.ProtocolProfile,
			PublicEndpointHost: cfg.PublicEndpointHost,
			PublicEndpointPort: cfg.PublicEndpointPort,
			EndpointReady:      false,
			PublicProbeEnabled: EffectivePublicProbeEnabled(cfg),
		},
	}
}

// Start launches the sing-box process.
func (m *Manager) Start(ctx context.Context) error {
	m.statusMu.Lock()
	m.status.LastError = ""

	if !m.cfg.Enabled {
		m.status.Status = string(StatusDisabled)
		m.status.EndpointReady = false
		m.statusMu.Unlock()
		return nil
	}

	if m.status.Status == string(StatusRunning) {
		m.status.Status = string(StatusRunning)
		m.statusMu.Unlock()
		return nil
	}

	m.status.Status = string(StatusStarting)
	m.statusMu.Unlock()

	if _, err := os.Stat(m.cfg.ConfigPath); err != nil {
		m.setFailed(fmt.Sprintf("config not found: %v", err))
		return fmt.Errorf("singbox config not found: %w", err)
	}

	m.cmdMu.Lock()
	defer m.cmdMu.Unlock()

	binPath := m.cfg.BinPath
	if binPath == "" {
		binPath = "sing-box"
	}

	args := []string{"run", "-c", m.cfg.ConfigPath}
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = m.cfg.WorkDir

	if m.cfg.LogPath != "" {
		f, err := os.OpenFile(m.cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			m.setFailed(fmt.Sprintf("open log: %v", err))
			return fmt.Errorf("open singbox log %s: %w", m.cfg.LogPath, err)
		}
		cmd.Stdout = f
		cmd.Stderr = f
	}

	if err := cmd.Start(); err != nil {
		m.setFailed(fmt.Sprintf("start: %v", err))
		return fmt.Errorf("start singbox: %w", err)
	}

	now := time.Now().Unix()
	m.statusMu.Lock()
	m.cmd = cmd
	m.status.PID = cmd.Process.Pid
	m.status.Status = string(StatusStarting)
	m.status.LastStartedAt = &now
	m.status.LastError = ""
	m.status.RestartCount++
	m.status.EndpointReady = false
	m.statusMu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := cmd.Wait(); err != nil {
			stopTime := time.Now().Unix()
			m.statusMu.Lock()
			if m.status.Status != string(StatusStopped) {
				m.status.Status = string(StatusFailed)
				m.status.LastStoppedAt = &stopTime
				m.status.EndpointReady = false
				if m.status.LastError == "" {
					m.status.LastError = fmt.Sprintf("exited: %v", err)
				}
			}
			m.statusMu.Unlock()
			log.Printf("[singbox] process exited: %v", err)
		}
	}()

	log.Printf("[singbox] started (pid=%d, config=%s)", cmd.Process.Pid, m.cfg.ConfigPath)
	m.statusMu.Lock()
	m.status.Status = string(StatusRunning)
	m.statusMu.Unlock()

	return nil
}

// Stop gracefully terminates the sing-box process.
func (m *Manager) Stop(ctx context.Context) error {
	m.cmdMu.Lock()
	cmd := m.cmd
	m.cmdMu.Unlock()

	if cmd == nil || cmd.Process == nil {
		m.statusMu.Lock()
		m.status.Status = string(StatusStopped)
		m.status.PID = 0
		m.status.EndpointReady = false
		now := time.Now().Unix()
		m.status.LastStoppedAt = &now
		m.statusMu.Unlock()
		return nil
	}

	log.Printf("[singbox] stopping (pid=%d)", cmd.Process.Pid)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("[singbox] sigterm failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(DefaultStopTimeout):
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("[singbox] kill failed: %v", err)
		}
		<-done
	case <-done:
	}

	now := time.Now().Unix()
	m.statusMu.Lock()
	m.cmd = nil
	m.status.PID = 0
	m.status.Status = string(StatusStopped)
	m.status.LastStoppedAt = &now
	m.status.LastError = ""
	m.status.EndpointReady = false
	m.statusMu.Unlock()

	log.Println("[singbox] stopped")
	return nil
}

// Restart stops the current process and starts a new one.
func (m *Manager) Restart(ctx context.Context) error {
	_ = m.Stop(ctx)
	return m.Start(ctx)
}

// IsRunning returns true if the sing-box process is alive.
func (m *Manager) IsRunning() bool {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.status.Status == string(StatusRunning)
}

// Status returns a copy of the current RuntimeStatus.
func (m *Manager) Status() RuntimeStatus {
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.status
}

// ApplyConfig processes a config update.
func (m *Manager) ApplyConfig(ctx context.Context, singCfg *SingboxConfig, configHash string) error {
	if !m.cfg.Enabled {
		return nil
	}

	if err := Render(singCfg); err != nil {
		m.setFailed(fmt.Sprintf("config render: %v", err))
		return fmt.Errorf("render singbox config: %w", err)
	}

	if configHash != "" && configHash == m.configHash {
		return nil
	}

	m.configHash = configHash

	// Propagate updated config fields to internal config so Start() uses them.
	if singCfg != nil {
		m.cfg.ListenHost = singCfg.ListenHost
		m.cfg.ListenPort = singCfg.ListenPort
		m.cfg.LogLevel = singCfg.LogLevel
		m.cfg.Transport = singCfg.Transport
		m.cfg.ProtocolProfile = singCfg.ProtocolProfile
		m.cfg.PublicEndpointHost = singCfg.PublicEndpointHost
		m.cfg.PublicEndpointPort = singCfg.PublicEndpointPort
		m.cfg.TunInterfaceName = singCfg.TunInterfaceName
		m.cfg.TunMTU = singCfg.TunMTU
		m.cfg.DNSEnabled = singCfg.DNSEnabled
		m.cfg.DNSStrategy = singCfg.DNSStrategy
		m.cfg.DNSServers = singCfg.DNSServers
		m.cfg.RouteGlobal = singCfg.RouteGlobal
		m.cfg.BypassLAN = singCfg.BypassLAN
		m.cfg.ProxyOutboundTag = singCfg.ProxyOutboundTag
		// TASK-NODEAGENT-SINGBOX-003: new fields.
		m.cfg.TLSEnabled = singCfg.TLSEnabled
		m.cfg.SNI = singCfg.SNI
		m.cfg.ALPN = singCfg.ALPN
		m.cfg.PublicProbeEnabled = singCfg.PublicProbeEnabled
		m.cfg.PublicProbeHost = singCfg.PublicProbeHost
		m.cfg.PublicProbePort = singCfg.PublicProbePort
		m.cfg.PublicProbeTimeoutMs = singCfg.PublicProbeTimeoutMs
		m.cfg.HealthCheckMode = singCfg.HealthCheckMode

		// Update runtime status with new fields.
		m.statusMu.Lock()
		m.status.Transport = singCfg.Transport
		m.status.ProtocolProfile = singCfg.ProtocolProfile
		m.status.PublicEndpointHost = singCfg.PublicEndpointHost
		m.status.PublicEndpointPort = singCfg.PublicEndpointPort
		m.status.PublicProbeEnabled = EffectivePublicProbeEnabled(singCfg)
		m.statusMu.Unlock()
	}

	if m.cfg.RestartOnConfigChange {
		return m.Restart(ctx)
	}
	return nil
}

// HealthCheck performs a single health check and updates the status.
// TASK-NODEAGENT-SINGBOX-003: integrates public probe checks.
func (m *Manager) HealthCheck() {
	m.statusMu.Lock()
	status := &m.status
	now := time.Now().Unix()
	status.LastHealthCheckAt = &now

	if !m.cfg.Enabled {
		status.Status = string(StatusDisabled)
		status.PID = 0
		status.LastError = ""
		status.EndpointReady = false
		status.PublicProbeOK = false
		status.PublicProbeLastErr = ""
		m.statusMu.Unlock()
		return
	}

	pid := status.PID
	m.statusMu.Unlock()

	processAlive := m.isProcessAlive(pid)
	if !processAlive && pid == 0 {
		m.statusMu.Lock()
		status.Status = string(StatusStopped)
		status.LastError = ""
		status.EndpointReady = false
		status.PublicProbeOK = false
		status.PublicProbeLastErr = ""
		m.statusMu.Unlock()
		return
	}

	if !processAlive {
		m.statusMu.Lock()
		status.Status = string(StatusFailed)
		status.PID = 0
		status.EndpointReady = false
		status.PublicProbeOK = false
		status.PublicProbeLastErr = ""
		if status.LastError == "" {
			status.LastError = "process not running"
		}
		m.statusMu.Unlock()
		return
	}

	// Process alive - check listen port.
	portOK := m.checkPort(m.cfg.ListenHost, m.cfg.ListenPort)

	// Check endpoint field readiness.
	endpointReady, _ := IsEndpointReady(m.cfg)

	// Public probe check.
	publicProbeEnabled := EffectivePublicProbeEnabled(m.cfg)
	probeOK, probeReason := PublicProbeHealthCheck(m.cfg)

	m.statusMu.Lock()
	status.PublicProbeEnabled = publicProbeEnabled
	status.PublicProbeOK = probeOK
	status.PublicProbeLastAt = &now
	if !probeOK && publicProbeEnabled {
		status.PublicProbeLastErr = probeReason
	} else {
		status.PublicProbeLastErr = ""
	}

	// endpoint_ready always requires a valid local listener and endpoint fields.
	// When a public probe is configured, it must also pass.
	epFieldOK := endpointReady
	if !epFieldOK || !portOK {
		status.EndpointReady = false
	} else if publicProbeEnabled {
		status.EndpointReady = probeOK
	} else {
		status.EndpointReady = true
	}

	// Compute singbox status.
	if portOK {
		status.Status = string(StatusRunning)
		status.LastError = ""
	} else {
		status.Status = string(StatusUnhealthy)
		status.LastError = "process running but port unreachable"
	}

	// Override status with endpoint_ready info.
	if !status.EndpointReady && status.Status == string(StatusRunning) {
		status.LastError = "endpoint_not_ready"
		// Status remains "running" while public endpoint readiness is degraded.
	}
	m.statusMu.Unlock()
}

// StartHealthLoop starts a periodic health check goroutine.
func (m *Manager) StartHealthLoop(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(DefaultHealthCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				m.HealthCheck()
				return
			case <-ticker.C:
				m.HealthCheck()
			}
		}
	}()
}

// WaitForShutdown blocks until all goroutines exit.
func (m *Manager) WaitForShutdown() {
	m.wg.Wait()
}

// ---- private helpers ----

func (m *Manager) setFailed(reason string) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	m.status.Status = string(StatusFailed)
	m.status.LastError = reason
	m.status.EndpointReady = false
	log.Printf("[singbox] %s", reason)
}

func (m *Manager) isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func (m *Manager) checkPort(host string, port int) bool {
	host = healthDialHost(host)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func healthDialHost(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return "127.0.0.1"
	}
	return host
}
