// Package config is agent-figma's non-secret configuration: profiles (token
// kind, expiry, declared seat and plan) and command defaults, stored as
// config.json under the XDG config dir.
package config

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/shhac/lib-agent-cli/creds"
	"github.com/shhac/lib-agent-cli/xdg"
)

const AppName = "agent-figma"

// Built-in defaults for the tunables in Defaults.
const (
	DefaultTimeoutMS   = 60000
	DefaultMaxRetries  = 2
	DefaultMaxWaitS    = 60
	DefaultCacheFreshS = 300
	DefaultPrefetch    = PrefetchRelated
)

const (
	PrefetchOff     = "off"
	PrefetchRelated = "related"
)

var PrefetchModes = []string{PrefetchOff, PrefetchRelated}

// ServeStale decides whether an older cached version may answer when the file
// has moved on: "auto" means yes where Tier 1 is monthly (low seats, Starter files).
const (
	ServeStaleAuto = "auto"
	ServeStaleOn   = "on"
	ServeStaleOff  = "off"
)

var ServeStaleModes = []string{ServeStaleAuto, ServeStaleOn, ServeStaleOff}

const (
	KindPAT  = "pat"
	KindPlan = "plan"
)

var Kinds = []string{KindPAT, KindPlan}

// Seats are the declarable seat types. Figma's 429s only distinguish "low"
// (Collab/View) from "high" (Full/Dev), which is all the budgets depend on.
var Seats = []string{"full", "dev", "collab", "view", "unknown"}

// Plans are the declarable plan tiers, matching X-Figma-Plan-Tier values.
var Plans = []string{"starter", "pro", "org", "enterprise", "student", "unknown"}

type Config struct {
	DefaultProfile string             `json:"default_profile,omitempty"`
	Defaults       Defaults           `json:"defaults,omitempty"`
	Profiles       map[string]Profile `json:"profiles"`
}

type Defaults struct {
	TimeoutMS   *int   `json:"timeout_ms,omitempty"`
	MaxRetries  *int   `json:"max_retries,omitempty"`
	MaxWaitS    *int   `json:"max_wait_s,omitempty"`
	CacheFreshS *int   `json:"cache_fresh_s,omitempty"`
	Prefetch    string `json:"prefetch,omitempty"`
	ServeStale  string `json:"serve_stale,omitempty"`
}

type Profile struct {
	Kind string `json:"kind,omitempty"`
	// Expires is the token's expiry date (YYYY-MM-DD) as the user reported it;
	// Figma does not expose it through the API.
	Expires   string `json:"expires,omitempty"`
	Seat      string `json:"seat,omitempty"`
	Plan      string `json:"plan,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

var (
	cache   *Config
	cacheMu sync.Mutex
)

func ConfigDir() string { return xdg.ConfigDir(AppName) }

func ConfigPath() string { return filepath.Join(ConfigDir(), "config.json") }

func store() creds.Store { return creds.Store{Path: ConfigPath()} }

// Read returns the config, cached for the life of the process.
func Read() *Config {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cache == nil {
		cache = load()
	}
	return cache
}

func load() *Config {
	var cfg Config
	if err := store().Load(&cfg); err != nil {
		return &Config{Profiles: map[string]Profile{}}
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return &cfg
}

// ClearCache forgets the cached config so the next Read goes to disk.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = nil
}

// Update applies mutate to a freshly loaded config under the store's lock, so
// concurrent invocations serialize instead of overwriting each other. An error
// from mutate aborts before anything is written.
func Update(mutate func(*Config) error) error {
	var cfg Config
	err := store().Update(&cfg, func() error {
		if cfg.Profiles == nil {
			cfg.Profiles = map[string]Profile{}
		}
		return mutate(&cfg)
	})
	ClearCache()
	return err
}

func StoreProfile(alias string, profile Profile, makeDefault bool) error {
	return Update(func(cfg *Config) error {
		cfg.Profiles[alias] = profile
		if makeDefault || cfg.DefaultProfile == "" {
			cfg.DefaultProfile = alias
		}
		return nil
	})
}

func UpdateProfile(alias string, update func(*Profile)) error {
	return Update(func(cfg *Config) error {
		profile, ok := cfg.Profiles[alias]
		if !ok {
			return fmt.Errorf("profile %q is not configured", alias)
		}
		update(&profile)
		cfg.Profiles[alias] = profile
		return nil
	})
}

func RemoveProfile(alias string) error {
	return Update(func(cfg *Config) error {
		delete(cfg.Profiles, alias)
		if cfg.DefaultProfile == alias {
			cfg.DefaultProfile = ""
			for name := range cfg.Profiles {
				cfg.DefaultProfile = name
				break
			}
		}
		return nil
	})
}

func SetDefault(alias string) error {
	return Update(func(cfg *Config) error {
		if _, ok := cfg.Profiles[alias]; !ok {
			return fmt.Errorf("profile %q is not configured", alias)
		}
		cfg.DefaultProfile = alias
		return nil
	})
}

// The Effective* methods return a tunable, falling back to its built-in default.
func (d Defaults) EffectiveTimeoutMS() int   { return orDefault(d.TimeoutMS, DefaultTimeoutMS) }
func (d Defaults) EffectiveMaxRetries() int  { return orDefault(d.MaxRetries, DefaultMaxRetries) }
func (d Defaults) EffectiveMaxWaitS() int    { return orDefault(d.MaxWaitS, DefaultMaxWaitS) }
func (d Defaults) EffectiveCacheFreshS() int { return orDefault(d.CacheFreshS, DefaultCacheFreshS) }

func (d Defaults) EffectivePrefetch() string {
	if d.Prefetch == "" {
		return DefaultPrefetch
	}
	return d.Prefetch
}

func (d Defaults) EffectiveServeStale() string {
	if d.ServeStale == "" {
		return ServeStaleAuto
	}
	return d.ServeStale
}

func orDefault(v *int, def int) int {
	if v == nil {
		return def
	}
	return *v
}
