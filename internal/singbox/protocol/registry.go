package protocol

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

const DefaultProfileName = "mixed"

var legacyProtocolProfiles = map[string]bool{
	"tcp_udp":  true,
	"tcp_only": true,
	"udp_only": true,
	"singbox":  true,
}

var transportBackedProfiles = map[string]bool{
	"mixed": true,
	"socks": true,
	"tun":   true,
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
	name := NormalizeProfileName(profile.Name())
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
	name = NormalizeProfileName(name)

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

var defaultRegistry = NewRegistry()

func DefaultRegistry() *Registry {
	return defaultRegistry
}

func Register(profile ProtocolProfile) error {
	return defaultRegistry.Register(profile)
}

func Get(name string) (ProtocolProfile, bool) {
	return defaultRegistry.Get(name)
}

func MustGet(name string) ProtocolProfile {
	return defaultRegistry.MustGet(name)
}

func NormalizeProfileName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || legacyProtocolProfiles[name] {
		return DefaultProfileName
	}
	return name
}

func ResolveProfileName(cfg ProtocolConfig) string {
	profile := strings.TrimSpace(strings.ToLower(cfg.Profile))
	if profile != "" {
		if legacyProtocolProfiles[profile] && transportBackedProfiles[strings.ToLower(cfg.Transport)] {
			return strings.ToLower(cfg.Transport)
		}
		return NormalizeProfileName(profile)
	}
	transport := strings.TrimSpace(strings.ToLower(cfg.Transport))
	if transportBackedProfiles[transport] {
		return transport
	}
	return DefaultProfileName
}
