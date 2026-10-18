// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resolve

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Defaults and bounds of the resolve cache knobs (#8092).
const (
	// DefaultLookupTimeout bounds one Go-resolver lookup, and with it the
	// longest a first command for a host waits for its pin.
	DefaultLookupTimeout = 5 * time.Second
	// DefaultMinGap is the shortest time between two lookups of one host.
	// It is not an environment setting; the cluster resolver itself caches
	// an answer for about 30s.
	DefaultMinGap = 30 * time.Second
	// DefaultRefreshInterval is how often a known host is re-resolved in
	// the background.
	DefaultRefreshInterval = time.Hour

	cacheSwitchEnv     = "ESHU_GIT_RESOLVE_CACHE"
	lookupSecondsEnv   = "ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS"
	refreshSecondsEnv  = "ESHU_GIT_RESOLVE_CACHE_REFRESH_SECONDS"
	maxLookupSeconds   = 60
	maxRefreshSeconds  = 24 * 60 * 60
	minRefreshSeconds  = 60
	defaultLookupSecs  = 5
	defaultRefreshSecs = 3600
)

// Settings is the environment contract of the cache. The zero value is a
// disabled cache with default durations, so a config built without
// SettingsFromEnv pins nothing.
type Settings struct {
	// Enabled is the ESHU_GIT_RESOLVE_CACHE kill switch.
	Enabled bool
	// LookupTimeout bounds one lookup. Zero means DefaultLookupTimeout.
	LookupTimeout time.Duration
	// MinGap is the per-host single-flight minimum gap. Zero means
	// DefaultMinGap.
	MinGap time.Duration
	// RefreshInterval is the background refresh period per host. Zero
	// means DefaultRefreshInterval.
	RefreshInterval time.Duration
}

func (s Settings) lookupTimeout() time.Duration {
	if s.LookupTimeout > 0 {
		return s.LookupTimeout
	}
	return DefaultLookupTimeout
}

func (s Settings) minGap() time.Duration {
	if s.MinGap > 0 {
		return s.MinGap
	}
	return DefaultMinGap
}

func (s Settings) refreshInterval() time.Duration {
	if s.RefreshInterval > 0 {
		return s.RefreshInterval
	}
	return DefaultRefreshInterval
}

// SettingsFromEnv reads ESHU_GIT_RESOLVE_CACHE (on or off, default on),
// ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS, and
// ESHU_GIT_RESOLVE_CACHE_REFRESH_SECONDS. Unset or blank keeps the default.
// A set value outside its documented spellings or range is a startup error
// that names the variable and the value.
func SettingsFromEnv(getenv func(string) string) (Settings, error) {
	settings := Settings{Enabled: true}
	switch value := strings.ToLower(strings.TrimSpace(getenv(cacheSwitchEnv))); value {
	case "", "on":
	case "off":
		settings.Enabled = false
	default:
		return Settings{}, fmt.Errorf("%s=%q: a set value must be on or off", cacheSwitchEnv, value)
	}
	lookup, err := secondsFromEnv(getenv, lookupSecondsEnv, defaultLookupSecs, 1, maxLookupSeconds)
	if err != nil {
		return Settings{}, err
	}
	refresh, err := secondsFromEnv(getenv, refreshSecondsEnv, defaultRefreshSecs, minRefreshSeconds, maxRefreshSeconds)
	if err != nil {
		return Settings{}, err
	}
	settings.LookupTimeout = time.Duration(lookup) * time.Second
	settings.MinGap = DefaultMinGap
	settings.RefreshInterval = time.Duration(refresh) * time.Second
	return settings, nil
}

func secondsFromEnv(getenv func(string) string, name string, def, minValue, maxValue int) (int, error) {
	trimmed := strings.TrimSpace(getenv(name))
	if trimmed == "" {
		return def, nil
	}
	seconds, err := strconv.Atoi(trimmed)
	if err != nil || seconds < minValue || seconds > maxValue {
		return 0, fmt.Errorf("%s=%q: a set value must be a whole number of seconds from %d to %d", name, trimmed, minValue, maxValue)
	}
	return seconds, nil
}

// proxyEnvNames are the process settings that put a proxy between curl and
// the remote. With any of them set, curl resolves the proxy's name rather
// than the remote's, so pinning the remote's addresses would be wrong. The
// git rather than environment spelling http.proxy can also be set in a
// repository config, which this package cannot see; a deployment that sets
// it there must turn the cache off.
var proxyEnvNames = []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"}

func proxyIsSet(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	for _, name := range proxyEnvNames {
		if strings.TrimSpace(getenv(name)) != "" {
			return true
		}
	}
	return false
}
