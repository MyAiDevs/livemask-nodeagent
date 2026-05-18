package protocol

import (
	"fmt"
	"sort"
	"sync"
)

const DefaultProfileName = "mixed"

var legacyProtocolProfiles = map[string]bool{
	"tcp_udp":  true,
	"tcp_only": true,
	"udp_only": true,
}

type Registry struct {
	mu       sync.RWMutex
	profiles map[string]ProtocolProfile
}

func NewRegistry() *Registry {
	return &Registry{profiles: make(map[string]ProtocolProfile)}
}

func (r *Registry) Register(profile ProtocolProfile) error {
	if profile == nil {
		return fmt.Errorf("protocol profile is nil")
	}
	name := profile.Name()
	if name == "" {
		return fmt.Errorf("protocol profile name is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.profiles[name]; exists {
		return fmt.Errorf("protocol profile %q already registered", name)
	}
	r.profiles[name] = profile
	return nil
}

func (r *Registry) Get(name string) (ProtocolProfile, bool) {
	if name == "" || legacyProtocolProfiles[name] {
		name = DefaultProfileName
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	profile, ok := r.profiles[name]
	return profile, ok
}

func (r *Registry) MustGet(name string) ProtocolProfile {
	profile, ok := r.Get(name)
	if !ok {
		panic(fmt.Sprintf("protocol profile %q is not registered", name))
	}
	return profile
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.profiles))
	for name := range r.profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var DefaultRegistry = NewRegistry()

func Register(profile ProtocolProfile) error {
	return DefaultRegistry.Register(profile)
}

func Get(name string) (ProtocolProfile, bool) {
	return DefaultRegistry.Get(name)
}

func MustGet(name string) ProtocolProfile {
	return DefaultRegistry.MustGet(name)
}

func ResolveProfileName(cfg ProtocolConfig) string {
	if cfg.Profile != "" && !legacyProtocolProfiles[cfg.Profile] {
		return cfg.Profile
	}
	if cfg.Transport != "" {
		return cfg.Transport
	}
	return DefaultProfileName
}
