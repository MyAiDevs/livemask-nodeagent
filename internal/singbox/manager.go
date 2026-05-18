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
// TASK-NODEAGENT-SINGBOX-001.
type Manager struct {
	cfg        *SingboxConfig
	status     RuntimeStatus
	statusMu   sync.RWMutex
	cmd        *exec.Cmd
	cmdMu      sync.Mutex
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	configHash string // tracks last applied config hash for restart-if-changed
}

// NewManager creates a new sing-box Manager using the supplied config.
func NewManager(cfg *SingboxConfig) *Manager {
	return &Manager{
		cfg: cfg,
		status: RuntimeStatus{
			Enabled:    cfg.Enabled,
			Status:     string(StatusDisabled),
			ConfigPath: cfg.ConfigPath,
			ListenHost: cfg.ListenHost,
			ListenPort: cfg.ListenPort,
		},
	}
}

// Start launches the sing-box process. If the manager is disabled or already
// running, it is a no-op.  Errors are returned but never cause a panic.
func (m *Manager) Start(ctx context.Context) error {
	m.statusMu.Lock()
	m.status.LastError = ""

	if !m.cfg.Enabled {
		m.status.Status = string(StatusDisabled)
		m.statusMu.Unlock()
		return nil
	}

	if m.status.Status == string(StatusRunning) {
		m.statusMu.Unlock()
		return nil
	}

	m.status.Status = string(StatusStarting)
	m.statusMu.Unlock()

	// Ensure config file exists.
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

	// Redirect stdout/stderr to log file.
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
	m.statusMu.Unlock()

	// Wait for process exit in background.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := cmd.Wait(); err != nil {
			// Process exited with error.
			stopTime := time.Now().Unix()
			m.statusMu.Lock()
			if m.status.Status != string(StatusStopped) {
				m.status.Status = string(StatusFailed)
				m.status.LastStoppedAt = &stopTime
				if m.status.LastError == "" {
					m.status.LastError = fmt.Sprintf("exited: %v", err)
				}
			}
			m.statusMu.Unlock()
			log.Printf("[singbox] process exited: %v", err)
		}
	}()

	log.Printf("[singbox] started (pid=%d, config=%s)", cmd.Process.Pid, m.cfg.ConfigPath)

	// Update status to running after a brief check.
	// The health check loop will settle the real status.
	m.statusMu.Lock()
	m.status.Status = string(StatusRunning)
	m.statusMu.Unlock()

	return nil
}

// Stop gracefully terminates the sing-box process. It sends SIGTERM and waits
// up to DefaultStopTimeout, then SIGKILL if still alive.
func (m *Manager) Stop(ctx context.Context) error {
	m.cmdMu.Lock()
	cmd := m.cmd
	m.cmdMu.Unlock()

	if cmd == nil || cmd.Process == nil {
		m.statusMu.Lock()
		m.status.Status = string(StatusStopped)
		m.status.PID = 0
		now := time.Now().Unix()
		m.status.LastStoppedAt = &now
		m.statusMu.Unlock()
		return nil
	}

	log.Printf("[singbox] stopping (pid=%d)", cmd.Process.Pid)

	// Send SIGTERM.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("[singbox] sigterm failed: %v", err)
	}

	// Wait up to DefaultStopTimeout.
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(DefaultStopTimeout):
		// Force kill.
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

// ApplyConfig processes a config update: renders the config file and restarts
// sing-box if enabled and the config changed.  On failure it preserves the
// old state and returns an error; the caller should set degraded flag.
func (m *Manager) ApplyConfig(ctx context.Context, singCfg *SingboxConfig, configHash string) error {
	if !m.cfg.Enabled {
		return nil
	}

	// Render the new config file.
	if err := Render(singCfg); err != nil {
		m.setFailed(fmt.Sprintf("config render: %v", err))
		return fmt.Errorf("render singbox config: %w", err)
	}

	// If hash is the same, no restart needed.
	if configHash != "" && configHash == m.configHash {
		return nil
	}

	m.configHash = configHash

	if m.cfg.RestartOnConfigChange {
		return m.Restart(ctx)
	}

	// No restart needed — just apply config for next manual restart.
	return nil
}

// HealthCheck performs a single health check and updates the status.
// It checks:
//  1. Process is alive
//  2. Listen port is reachable
func (m *Manager) HealthCheck() {
	m.statusMu.Lock()
	status := &m.status
	now := time.Now().Unix()
	status.LastHealthCheckAt = &now

	if !m.cfg.Enabled {
		status.Status = string(StatusDisabled)
		status.PID = 0
		status.LastError = ""
		m.statusMu.Unlock()
		return
	}

	// Check process.
	pid := status.PID
	m.statusMu.Unlock()

	processAlive := m.isProcessAlive(pid)
	if !processAlive && pid == 0 {
		// Expected stopped state.
		m.statusMu.Lock()
		status.Status = string(StatusStopped)
		status.LastError = ""
		m.statusMu.Unlock()
		return
	}

	if !processAlive {
		m.statusMu.Lock()
		status.Status = string(StatusFailed)
		status.PID = 0
		if status.LastError == "" {
			status.LastError = "process not running"
		}
		m.statusMu.Unlock()
		return
	}

	// Process is alive — check port.
	if m.checkPort(status.ListenHost, status.ListenPort) {
		m.statusMu.Lock()
		status.Status = string(StatusRunning)
		status.LastError = ""
		m.statusMu.Unlock()
		return
	}

	// Process alive but port unreachable.
	m.statusMu.Lock()
	status.Status = string(StatusUnhealthy)
	status.LastError = "process running but port unreachable"
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
				m.HealthCheck() // final check
				return
			case <-ticker.C:
				m.HealthCheck()
			}
		}
	}()
}

// WaitForShutdown blocks until all goroutines exit. Call after context cancel.
func (m *Manager) WaitForShutdown() {
	m.wg.Wait()
}

// ---- private helpers ----

func (m *Manager) setFailed(reason string) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	m.status.Status = string(StatusFailed)
	m.status.LastError = reason
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
	// Sending signal 0 checks if process is alive without actually signalling.
	return process.Signal(syscall.Signal(0)) == nil
}

func (m *Manager) checkPort(host string, port int) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
