// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigArtifactDeriveConfigTimeout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		set     bool
		want    time.Duration
		wantErr bool
	}{
		{name: "unset", want: 10 * time.Second},
		{name: "blank", raw: "   ", set: true, want: 10 * time.Second},
		{name: "ten seconds", raw: "10s", set: true, want: 10 * time.Second},
		{name: "two seconds", raw: "2s", set: true, want: 2 * time.Second},
		{name: "trimmed", raw: " 1500ms ", set: true, want: 1500 * time.Millisecond},
		{name: "zero", raw: "0", set: true, wantErr: true},
		{name: "zero with unit", raw: "0s", set: true, wantErr: true},
		{name: "negative", raw: "-1s", set: true, wantErr: true},
		{name: "unparsable", raw: "abc", set: true, wantErr: true},
		{name: "bare number", raw: "10", set: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := LoadConfigArtifactDeriveConfig(deriveEnv(map[string]string{}, ConfigArtifactDeriveTimeoutEnv, tc.raw, tc.set))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("LoadConfigArtifactDeriveConfig(%q) error = nil, want error", tc.raw)
				}
				if !strings.Contains(err.Error(), ConfigArtifactDeriveTimeoutEnv) {
					t.Fatalf("error = %v, want it to name %s", err, ConfigArtifactDeriveTimeoutEnv)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfigArtifactDeriveConfig(%q) error = %v, want nil", tc.raw, err)
			}
			if cfg.Timeout != tc.want {
				t.Fatalf("Timeout = %s, want %s", cfg.Timeout, tc.want)
			}
			if !cfg.Enabled {
				t.Fatal("Enabled = false with the flag unset, want true")
			}
		})
	}
}

func TestLoadConfigArtifactDeriveConfigEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		set     bool
		want    bool
		wantErr bool
	}{
		{name: "unset", want: true},
		{name: "blank", raw: " ", set: true, want: true},
		{name: "true", raw: "true", set: true, want: true},
		{name: "false", raw: "false", set: true, want: false},
		{name: "trimmed false", raw: " false ", set: true, want: false},
		{name: "unparsable", raw: "abc", set: true, wantErr: true},
		{name: "yes is not a bool", raw: "yes", set: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := LoadConfigArtifactDeriveConfig(deriveEnv(map[string]string{}, ConfigArtifactDeriveEnabledEnv, tc.raw, tc.set))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("LoadConfigArtifactDeriveConfig(%q) error = nil, want error", tc.raw)
				}
				if !strings.Contains(err.Error(), ConfigArtifactDeriveEnabledEnv) {
					t.Fatalf("error = %v, want it to name %s", err, ConfigArtifactDeriveEnabledEnv)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfigArtifactDeriveConfig(%q) error = %v, want nil", tc.raw, err)
			}
			if cfg.Enabled != tc.want {
				t.Fatalf("Enabled = %t, want %t", cfg.Enabled, tc.want)
			}
			if cfg.Timeout != DefaultConfigArtifactDeriveTimeout {
				t.Fatalf("Timeout = %s with the timeout unset, want %s", cfg.Timeout, DefaultConfigArtifactDeriveTimeout)
			}
		})
	}
}

// TestLoadConfigArtifactDeriveConfigDisabledStillValidatesTimeout pins that
// the kill switch does not hide a bad timeout: a process started with the
// derive off fails on the same value that would fail it with the derive on,
// so flipping the switch back cannot surface a latent startup error.
func TestLoadConfigArtifactDeriveConfigDisabledStillValidatesTimeout(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		ConfigArtifactDeriveEnabledEnv: "false",
		ConfigArtifactDeriveTimeoutEnv: "0",
	}
	if _, err := LoadConfigArtifactDeriveConfig(func(key string) string { return env[key] }); err == nil {
		t.Fatal("LoadConfigArtifactDeriveConfig() error = nil with enabled=false and timeout=0, want error")
	}
}

func TestLoadConfigArtifactDeriveConfigNilGetenv(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfigArtifactDeriveConfig(nil)
	if err != nil {
		t.Fatalf("LoadConfigArtifactDeriveConfig(nil) error = %v, want nil", err)
	}
	if cfg.Timeout != 10*time.Second || !cfg.Enabled {
		t.Fatalf("LoadConfigArtifactDeriveConfig(nil) = %+v, want 10s and enabled", cfg)
	}
}

func TestConfigArtifactDeriveEnvNames(t *testing.T) {
	t.Parallel()

	if ConfigArtifactDeriveTimeoutEnv != "ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT" {
		t.Fatalf("ConfigArtifactDeriveTimeoutEnv = %q", ConfigArtifactDeriveTimeoutEnv)
	}
	if ConfigArtifactDeriveEnabledEnv != "ESHU_CONFIG_ARTIFACT_DERIVE_ENABLED" {
		t.Fatalf("ConfigArtifactDeriveEnabledEnv = %q", ConfigArtifactDeriveEnabledEnv)
	}
}

// deriveEnv returns a getenv over env with key set to raw when set is true.
func deriveEnv(env map[string]string, key, raw string, set bool) func(string) string {
	if set {
		env[key] = raw
	}
	return func(name string) string { return env[name] }
}
