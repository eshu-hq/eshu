// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestLoadSourceBackpressureConfigDisabledByDefault proves the zero
// value: no ESHU_SOURCE_BACKPRESSURE_* variable set leaves the leg off
// without touching storage.
func TestLoadSourceBackpressureConfigDisabledByDefault(t *testing.T) {
	t.Parallel()

	config, err := LoadSourceBackpressureConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("LoadSourceBackpressureConfig() error = %v, want nil", err)
	}
	if config != (workflow.SourceBackpressureConfig{}) {
		t.Fatalf("config = %+v, want zero value", config)
	}
}

// TestLoadSourceBackpressureConfigParsesFullSet proves a complete
// operator set loads, and TestLoadSourceBackpressureConfigPartialFails
// proves a half-configured throttle fails startup instead of running
// half-throttled.
func TestLoadSourceBackpressureConfigParsesFullSet(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		SourceBackpressureEngageAgeEnv:     "1h",
		SourceBackpressureReleaseAgeEnv:    "15m",
		SourceBackpressureDepthFloorEnv:    "25",
		SourceBackpressureGrantDelayEnv:    "2s",
		SourceBackpressureMaxEngagementEnv: "30m",
		SourceBackpressureSignalCacheEnv:   "30s",
	}
	config, err := LoadSourceBackpressureConfig(func(name string) string { return env[name] })
	if err != nil {
		t.Fatalf("LoadSourceBackpressureConfig() error = %v, want nil", err)
	}
	if !config.Enabled() {
		t.Fatal("config.Enabled() = false, want true for a full set")
	}
	if config.EngageAge != time.Hour || config.SignalCacheTTL != 30*time.Second {
		t.Fatalf("config = %+v, want parsed durations", config)
	}
}

func TestLoadSourceBackpressureConfigPartialFails(t *testing.T) {
	t.Parallel()

	env := map[string]string{SourceBackpressureEngageAgeEnv: "1h"}
	if _, err := LoadSourceBackpressureConfig(func(name string) string { return env[name] }); err == nil {
		t.Fatal("partial config loads, want startup refusal")
	}
}
