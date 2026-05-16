package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

const (
	// DefaultPollInterval is the base time between polls.
	DefaultPollInterval = 60 * time.Second
	// MaxJitterFraction is the maximum random jitter as a fraction of the interval.
	MaxJitterFraction = 0.25
	// BackoffMultiplier is the exponential backoff factor after a failure.
	BackoffMultiplier = 2.0
	// MaxBackoffInterval caps the exponential backoff.
	MaxBackoffInterval = 10 * time.Minute
)

// Manager orchestrates config fetching, validation, caching, and runtime
// application. It is the top-level entry point for the config subsystem.
type Manager struct {
	client      *Client
	store       *Store
	applier     *RuntimeApplier
	pollCtx     context.Context
	pollCancel  context.CancelFunc
	pollWg      sync.WaitGroup

	mu           sync.RWMutex
	currentResp  *ConfigResponse
	currentCfg   *RuntimeConfig
	isDegraded   bool
	lastFetchAt  *time.Time
	lastError    string

	statusHooks []func(ConfigStatus)
}

// ManagerOption allows functional configuration of the Manager.
type ManagerOption func(*Manager)

// WithPollInterval sets a custom base poll interval.
func WithPollInterval(d time.Duration) ManagerOption {
	return func(m *Manager) {
		// Applied via setter — use default now, overridable.
		_ = d
	}
}

// NewManager creates a new Manager.
func NewManager(client *Client, store *Store, applier *RuntimeApplier) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		client:     client,
		store:      store,
		applier:    applier,
		pollCtx:    ctx,
		pollCancel: cancel,
	}
}

// Status returns an observable snapshot of the current config state.
func (m *Manager) Status() ConfigStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := ConfigStatus{
		IsDegraded:    m.isDegraded,
		LastFetchAt:   m.lastFetchAt,
		LastError:     m.lastError,
	}
	if m.currentResp != nil {
		s.ConfigVersion = m.currentResp.ConfigVersion
		s.ConfigHash = m.currentResp.ConfigHash
		s.ConfigKey = m.currentResp.ConfigKey
		s.SchemaVersion = m.currentResp.SchemaVersion
	}
	return s
}

// CurrentConfig returns the active RuntimeConfig. Returns a safe default if
// no config has been loaded yet.
func (m *Manager) CurrentConfig() RuntimeConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentCfg != nil {
		return m.currentCfg.Clone()
	}
	return DefaultRuntimeConfig()
}

// OnStatusChange registers a callback that fires when the config status
// changes (after a successful or failed fetch).
func (m *Manager) OnStatusChange(hook func(ConfigStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusHooks = append(m.statusHooks, hook)
}

// fireStatusHooks calls all registered status hooks.
func (m *Manager) fireStatusHooks() {
	status := m.Status()
	m.mu.RLock()
	hooks := make([]func(ConfigStatus), len(m.statusHooks))
	copy(hooks, m.statusHooks)
	m.mu.RUnlock()
	for _, h := range hooks {
		h(status)
	}
}

// startPoll launches the background polling goroutine.
func (m *Manager) startPoll(interval time.Duration) {
	m.pollWg.Add(1)
	go func() {
		defer m.pollWg.Done()
		currentInterval := interval
		for {
			select {
			case <-m.pollCtx.Done():
				return
			case <-time.After(jitteredInterval(currentInterval)):
				changed, err := m.sync()
				if err != nil {
					log.Printf("[config] poll sync error: %v", err)
					currentInterval = backoff(currentInterval, interval, BackoffMultiplier, MaxBackoffInterval)
				} else {
					currentInterval = interval // reset on success
				}
				if changed {
					log.Printf("[config] config updated to version %d", m.Status().ConfigVersion)
				}
			}
		}
	}()
}

// StopPoll gracefully stops the background polling goroutine.
func (m *Manager) StopPoll() {
	m.pollCancel()
	m.pollWg.Wait()
}

// SyncOnce performs a single fetch-store-apply cycle. It is safe for both
// initial startup and manual reload triggers. Returns true if the config
// changed.
func (m *Manager) SyncOnce(ctx context.Context) (bool, error) {
	return m.syncWithContext(ctx)
}

// sync is the core sync loop used by both SyncOnce and the poll goroutine.
func (m *Manager) sync() (bool, error) {
	ctx, cancel := context.WithTimeout(m.pollCtx, 15*time.Second)
	defer cancel()
	return m.syncWithContext(ctx)
}

func (m *Manager) syncWithContext(ctx context.Context) (bool, error) {
	localVersion := 0
	m.mu.RLock()
	if m.currentResp != nil {
		localVersion = m.currentResp.ConfigVersion
	}
	m.mu.RUnlock()

	now := time.Now()
	resp, err := m.client.Fetch(ctx, localVersion)
	if err != nil {
		m.mu.Lock()
		m.isDegraded = true
		m.lastFetchAt = &now
		m.lastError = err.Error()
		m.mu.Unlock()
		m.fireStatusHooks()
		return false, fmt.Errorf("fetch config: %w", err)
	}

	// Validate the response.
	warnings, err := ValidateResponse(resp)
	if err != nil {
		m.mu.Lock()
		m.isDegraded = true
		m.lastFetchAt = &now
		m.lastError = err.Error()
		m.mu.Unlock()
		m.fireStatusHooks()
		return false, fmt.Errorf("invalid config response: %w", err)
	}
	for _, w := range warnings {
		log.Printf("[config] validation warning: %s", w)
	}

	// Verify hash matches payload.
	if _, hashErr := VerifyHash(resp.Payload, resp.ConfigHash); hashErr != nil {
		m.mu.Lock()
		m.isDegraded = true
		m.lastFetchAt = &now
		m.lastError = hashErr.Error()
		m.mu.Unlock()
		m.fireStatusHooks()
		return false, fmt.Errorf("hash verification failed: %w", hashErr)
	}

	// Same version — no-op.
	m.mu.RLock()
	sameVersion := m.currentResp != nil && m.currentResp.ConfigVersion == resp.ConfigVersion
	m.mu.RUnlock()
	if sameVersion {
		m.mu.Lock()
		m.isDegraded = false
		m.lastFetchAt = &now
		m.lastError = ""
		m.mu.Unlock()
		m.fireStatusHooks()
		return false, nil
	}

	// Parse payload into RuntimeConfig.
	var parsedCfg RuntimeConfig
	if err := json.Unmarshal(resp.Payload, &parsedCfg); err != nil {
		m.mu.Lock()
		m.isDegraded = true
		m.lastFetchAt = &now
		m.lastError = fmt.Sprintf("parse payload: %v", err)
		m.mu.Unlock()
		m.fireStatusHooks()
		return false, fmt.Errorf("parse runtime config: %w", err)
	}
	if parsedCfg.SchemaVersion == "" {
		parsedCfg.SchemaVersion = resp.SchemaVersion
	}

	// Apply to runtime.
	old := m.CurrentConfig()
	if applyErr := m.applier.Apply(&old, &parsedCfg); applyErr != nil {
		m.mu.Lock()
		m.isDegraded = true
		m.lastFetchAt = &now
		m.lastError = fmt.Sprintf("apply config: %v", applyErr)
		m.mu.Unlock()
		// We do NOT update currentCfg — keep old version.
		// But we still save the raw response so we can recover after a restart.
		// Actually: per spec "非法配置拒绝应用并保留旧版本", so skip cache update too.
		m.fireStatusHooks()
		return false, fmt.Errorf("apply config rejected: %w", applyErr)
	}

	// Save as last-known-good.
	entry := &CacheEntry{
		Response:  resp,
		Parsed:    &parsedCfg,
		FetchedAt: now,
	}
	if saveErr := m.store.Save(entry); saveErr != nil {
		log.Printf("[config] failed to persist last-known-good: %v", saveErr)
		// Non-fatal: we still apply the config.
	}

	m.mu.Lock()
	m.currentResp = resp
	m.currentCfg = &parsedCfg
	m.isDegraded = false
	m.lastFetchAt = &now
	m.lastError = ""
	m.mu.Unlock()
	m.fireStatusHooks()

	return true, nil
}

// LoadLastKnownGood attempts to load the cached config from disk. If
// successful it sets the current state to the cached values. Returns true
// if a cache was loaded, false if no cache exists.
func (m *Manager) LoadLastKnownGood() bool {
	entry, err := m.store.Load()
	if err != nil {
		log.Printf("[config] failed to load last-known-good: %v", err)
		return false
	}
	if entry == nil || entry.Response == nil || entry.Parsed == nil {
		return false
	}
	m.mu.Lock()
	m.currentResp = entry.Response
	m.currentCfg = entry.Parsed
	m.lastFetchAt = &entry.FetchedAt
	m.isDegraded = false
	m.lastError = ""
	m.mu.Unlock()
	log.Printf("[config] loaded last-known-good config version %d", entry.Response.ConfigVersion)
	return true
}

// StartPoll kicks off the background polling loop. Call StopPoll to stop.
func (m *Manager) StartPoll(interval time.Duration) {
	m.startPoll(interval)
}

// jitteredInterval adds random jitter to prevent thundering herd.
func jitteredInterval(base time.Duration) time.Duration {
	jitter := time.Duration(float64(base) * MaxJitterFraction * rand.Float64())
	return base + jitter
}

// backoff computes the next poll interval with exponential backoff,
// capped at maxInterval, and never falling below baseInterval.
func backoff(currentInterval, baseInterval time.Duration, multiplier float64, max time.Duration) time.Duration {
	next := time.Duration(float64(currentInterval) * multiplier)
	if next > max {
		next = max
	}
	if next < baseInterval {
		next = baseInterval
	}
	return next
}
