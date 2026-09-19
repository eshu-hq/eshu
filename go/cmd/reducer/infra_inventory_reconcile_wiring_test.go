// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"
)

func TestLoadInfraInventoryReconcileConfigDefaults(t *testing.T) {
	cfg := loadInfraInventoryReconcileConfig(func(string) string { return "" })
	if !cfg.Enabled {
		t.Fatal("Enabled = false, want the reconcile on by default")
	}
	if cfg.Runner.PollInterval != 5*time.Minute || cfg.Runner.RepoBudget != 500 {
		t.Fatalf("defaults = %+v, want 5m interval and 500 repositories", cfg.Runner)
	}
}

func TestLoadInfraInventoryReconcileConfigOverridesAndInvalidValues(t *testing.T) {
	env := map[string]string{
		infraInventoryReconcileEnabledEnv:    "false",
		infraInventoryReconcileIntervalEnv:   "90s",
		infraInventoryReconcileRepoBudgetEnv: "25",
	}
	cfg := loadInfraInventoryReconcileConfig(func(key string) string { return env[key] })
	if cfg.Enabled || cfg.Runner.PollInterval != 90*time.Second || cfg.Runner.RepoBudget != 25 {
		t.Fatalf("overrides = %+v, want disabled, 90s, 25", cfg)
	}

	env[infraInventoryReconcileIntervalEnv] = "-1s"
	env[infraInventoryReconcileRepoBudgetEnv] = "zero"
	cfg = loadInfraInventoryReconcileConfig(func(key string) string { return env[key] })
	if cfg.Runner.PollInterval != 5*time.Minute || cfg.Runner.RepoBudget != 500 {
		t.Fatalf("invalid values = %+v, want the defaults", cfg.Runner)
	}
}

func TestInfraInventoryReconcileRunnerFor(t *testing.T) {
	disabled := infraInventoryReconcileRunnerFor(func(key string) string {
		if key == infraInventoryReconcileEnabledEnv {
			return "false"
		}
		return ""
	}, &fakeReducerDB{}, nil, nil, nil)
	if disabled != nil {
		t.Fatal("runner = non-nil, want nil when disabled")
	}

	enabled := infraInventoryReconcileRunnerFor(func(string) string { return "" }, &fakeReducerDB{}, nil, nil, nil)
	if enabled == nil || enabled.Reconciler == nil {
		t.Fatal("runner or reconciler = nil, want the Postgres reconciler wired by default")
	}
	if enabled.Config.RepoBudget != 500 {
		t.Fatalf("RepoBudget = %d, want 500", enabled.Config.RepoBudget)
	}
}
