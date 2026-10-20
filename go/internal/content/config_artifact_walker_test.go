// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"testing"
)

func walkerEnv(getenv map[string]string) func(string) string {
	return func(key string) string { return getenv[key] }
}

// TestLoadConfigArtifactWalkerConfigDefaults pins the walker's start-time
// settings: enabled with 4 workers when the environment says nothing, and a
// nil getenv reads the same defaults.
func TestLoadConfigArtifactWalkerConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfigArtifactWalkerConfig(walkerEnv(nil))
	if err != nil {
		t.Fatalf("LoadConfigArtifactWalkerConfig(empty) error = %v", err)
	}
	if !cfg.Enabled || cfg.Workers != 4 {
		t.Fatalf("LoadConfigArtifactWalkerConfig(empty) = %+v, want {Enabled:true Workers:4}", cfg)
	}
	cfg, err = LoadConfigArtifactWalkerConfig(nil)
	if err != nil {
		t.Fatalf("LoadConfigArtifactWalkerConfig(nil) error = %v", err)
	}
	if !cfg.Enabled || cfg.Workers != 4 {
		t.Fatalf("LoadConfigArtifactWalkerConfig(nil) = %+v, want {Enabled:true Workers:4}", cfg)
	}
	if DefaultConfigArtifactWalkerWorkers != 4 {
		t.Fatalf("DefaultConfigArtifactWalkerWorkers = %d, want 4 (the record's K)", DefaultConfigArtifactWalkerWorkers)
	}
}

// TestLoadConfigArtifactWalkerConfigParses pins parsing: explicit values win,
// blanks fall back to defaults, and the variable names are the record's.
func TestLoadConfigArtifactWalkerConfigParses(t *testing.T) {
	t.Parallel()

	if ConfigArtifactWalkerEnabledEnv != "ESHU_CONFIG_ARTIFACT_WALKER_ENABLED" {
		t.Fatalf("enabled var = %q", ConfigArtifactWalkerEnabledEnv)
	}
	if ConfigArtifactWalkerWorkersEnv != "ESHU_CONFIG_ARTIFACT_WALKER_WORKERS" {
		t.Fatalf("workers var = %q", ConfigArtifactWalkerWorkersEnv)
	}
	cfg, err := LoadConfigArtifactWalkerConfig(walkerEnv(map[string]string{
		ConfigArtifactWalkerEnabledEnv: "false",
		ConfigArtifactWalkerWorkersEnv: "8",
	}))
	if err != nil {
		t.Fatalf("LoadConfigArtifactWalkerConfig(valid) error = %v", err)
	}
	if cfg.Enabled || cfg.Workers != 8 {
		t.Fatalf("LoadConfigArtifactWalkerConfig(valid) = %+v, want {Enabled:false Workers:8}", cfg)
	}
	cfg, err = LoadConfigArtifactWalkerConfig(walkerEnv(map[string]string{
		ConfigArtifactWalkerEnabledEnv: "  ",
		ConfigArtifactWalkerWorkersEnv: "",
	}))
	if err != nil {
		t.Fatalf("LoadConfigArtifactWalkerConfig(blank) error = %v", err)
	}
	if !cfg.Enabled || cfg.Workers != 4 {
		t.Fatalf("LoadConfigArtifactWalkerConfig(blank) = %+v, want defaults", cfg)
	}
}

// TestLoadConfigArtifactWalkerConfigRejects pins fail-fast startup: an
// unparsable switch, an unparsable worker count, or a worker count below 1
// is an error, even when the walker is disabled, so flipping the switch back
// on cannot surface a latent startup error.
func TestLoadConfigArtifactWalkerConfigRejects(t *testing.T) {
	t.Parallel()

	for _, env := range []map[string]string{
		{ConfigArtifactWalkerEnabledEnv: "maybe"},
		{ConfigArtifactWalkerWorkersEnv: "four"},
		{ConfigArtifactWalkerWorkersEnv: "0"},
		{ConfigArtifactWalkerWorkersEnv: "-2"},
		{ConfigArtifactWalkerWorkersEnv: "4.5"},
		{
			ConfigArtifactWalkerEnabledEnv: "false",
			ConfigArtifactWalkerWorkersEnv: "0",
		},
	} {
		if _, err := LoadConfigArtifactWalkerConfig(walkerEnv(env)); err == nil {
			t.Fatalf("LoadConfigArtifactWalkerConfig(%v) = nil, want an error", env)
		}
	}
}
