// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"testing"
	"time"
)

func TestLoadSemanticProviderWorkerConfigDefaultsOff(t *testing.T) {
	t.Parallel()

	cfg, err := LoadProviderWorkerConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("LoadProviderWorkerConfig() error = %v, want nil", err)
	}
	if cfg.Enabled {
		t.Fatal("Enabled = true, want false by default")
	}
	if cfg.ExecutionEnabled {
		t.Fatal("ExecutionEnabled = true, want false by default")
	}
}

func TestLoadSemanticProviderWorkerConfigParsesScopesAndFlags(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		EnvProviderWorkerEnabled:          "true",
		EnvProviderExecutionEnabled:       "false",
		EnvProviderWorkerScopeIDsJSON:     `["repository:eshu", "repository:eshu", " "]`,
		EnvProviderWorkerLeaseTTL:         "90s",
		EnvProviderWorkerMaxClaimsPerPass: "64",
	}
	cfg, err := LoadProviderWorkerConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("LoadProviderWorkerConfig() error = %v, want nil", err)
	}
	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.ExecutionEnabled {
		t.Fatal("ExecutionEnabled = true, want false")
	}
	if got, want := len(cfg.ScopeIDs), 1; got != want {
		t.Fatalf("ScopeIDs = %#v, want one deduplicated non-blank scope", cfg.ScopeIDs)
	}
	if got, want := cfg.LeaseTTL, 90*time.Second; got != want {
		t.Fatalf("LeaseTTL = %v, want %v", got, want)
	}
	if got, want := cfg.MaxClaimsPerPass, 64; got != want {
		t.Fatalf("MaxClaimsPerPass = %d, want %d", got, want)
	}
}

func TestLoadSemanticProviderWorkerConfigEnabledRequiresScopes(t *testing.T) {
	t.Parallel()

	env := map[string]string{EnvProviderWorkerEnabled: "true"}
	if _, err := LoadProviderWorkerConfig(func(k string) string { return env[k] }); err == nil {
		t.Fatal("LoadProviderWorkerConfig() error = nil, want missing-scope error")
	}
}
