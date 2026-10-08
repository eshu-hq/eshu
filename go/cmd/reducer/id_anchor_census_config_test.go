// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestIDAnchorCensusConfig(t *testing.T) {
	cfg := loadIDAnchorCensusConfig(func(string) string { return "" })
	if !cfg.Enabled || cfg.PollInterval != time.Hour || cfg.Timeout != 2*time.Minute {
		t.Fatalf("defaults = %+v, want enabled, 1h, 2m", cfg)
	}
	env := map[string]string{
		idAnchorCensusEnabledEnv: "false", idAnchorCensusPollIntervalEnv: "15m", idAnchorCensusTimeoutEnv: "30s",
	}
	cfg = loadIDAnchorCensusConfig(func(k string) string { return env[k] })
	if cfg.Enabled || cfg.PollInterval != 15*time.Minute || cfg.Timeout != 30*time.Second {
		t.Fatalf("overrides = %+v", cfg)
	}
}

// The labeled anchor is a Neo4j statement, so the census runs only there.
func TestIDAnchorCensusStartsOnlyOnNeo4j(t *testing.T) {
	cfg := idAnchorCensusConfig{Enabled: true, PollInterval: time.Hour, Timeout: time.Second}
	tests := []struct {
		name    string
		cfg     idAnchorCensusConfig
		backend runtimecfg.GraphBackend
		want    bool
	}{
		{"neo4j enabled", cfg, runtimecfg.GraphBackendNeo4j, true},
		{"nornicdb", cfg, runtimecfg.GraphBackendNornicDB, false},
		{"neo4j disabled", idAnchorCensusConfig{}, runtimecfg.GraphBackendNeo4j, false},
	}
	for _, tc := range tests {
		if got := idAnchorCensusShouldRun(tc.cfg, tc.backend); got != tc.want {
			t.Errorf("%s: shouldRun = %t, want %t", tc.name, got, tc.want)
		}
	}
}
