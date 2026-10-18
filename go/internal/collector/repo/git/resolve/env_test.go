// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resolve

import (
	"strings"
	"testing"
	"time"
)

func envOf(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// TestSettingsFromEnvDefaults: unset keeps the cache on with the documented
// 5s lookup deadline and hourly refresh.
func TestSettingsFromEnvDefaults(t *testing.T) {
	settings, err := SettingsFromEnv(envOf(nil))
	if err != nil {
		t.Fatalf("SettingsFromEnv: %v", err)
	}
	if !settings.Enabled || settings.LookupTimeout != 5*time.Second || settings.RefreshInterval != time.Hour || settings.MinGap != 30*time.Second {
		t.Fatalf("settings = %+v, want on/5s/1h/30s", settings)
	}
}

// TestSettingsFromEnvParsesAndRejects: every knob parses its documented
// spellings and a bad value is a startup error naming variable and value.
func TestSettingsFromEnvParsesAndRejects(t *testing.T) {
	settings, err := SettingsFromEnv(envOf(map[string]string{
		"ESHU_GIT_RESOLVE_CACHE":                 "off",
		"ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS":  "9",
		"ESHU_GIT_RESOLVE_CACHE_REFRESH_SECONDS": "7200",
	}))
	if err != nil {
		t.Fatalf("SettingsFromEnv: %v", err)
	}
	if settings.Enabled || settings.LookupTimeout != 9*time.Second || settings.RefreshInterval != 2*time.Hour {
		t.Fatalf("settings = %+v, want off/9s/2h", settings)
	}

	for name, vars := range map[string]map[string]string{
		"switch garbage":  {"ESHU_GIT_RESOLVE_CACHE": "maybe"},
		"lookup zero":     {"ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS": "0"},
		"lookup fraction": {"ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS": "1.5"},
		"lookup too big":  {"ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS": "3600"},
		"refresh zero":    {"ESHU_GIT_RESOLVE_CACHE_REFRESH_SECONDS": "0"},
		"refresh text":    {"ESHU_GIT_RESOLVE_CACHE_REFRESH_SECONDS": "hourly"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := SettingsFromEnv(envOf(vars))
			if err == nil {
				t.Fatalf("SettingsFromEnv(%v) = nil error, want a startup error", vars)
			}
			for key := range vars {
				if !strings.Contains(err.Error(), key) {
					t.Fatalf("error = %v, want it to name %s", err, key)
				}
			}
		})
	}
}
