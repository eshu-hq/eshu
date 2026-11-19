// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestCodeReachabilityProjectionIdleTuningDefaults pins the #10870 idle
// defaults: a 30s backoff cap (the intent-completion-driven pickup bound), a
// 15m full-query audit (every other transition bound), and the probe enabled.
func TestCodeReachabilityProjectionIdleTuningDefaults(t *testing.T) {
	t.Parallel()

	maxPoll, audit, disabled := loadCodeReachabilityProjectionIdleTuning(activationEnv(map[string]string{}))
	if maxPoll != 30*time.Second {
		t.Fatalf("maxPollInterval = %v, want 30s", maxPoll)
	}
	if audit != 15*time.Minute {
		t.Fatalf("auditInterval = %v, want 15m", audit)
	}
	if disabled {
		t.Fatal("idleProbeDisabled = true, want false (probe enabled by default)")
	}
}

// TestCodeReachabilityProjectionIdleTuningFlowsFromEnv pins the three #10870
// knobs flowing from the environment, with unparsable values falling back to
// the package defaults without failing startup.
func TestCodeReachabilityProjectionIdleTuningFlowsFromEnv(t *testing.T) {
	t.Parallel()

	maxPoll, audit, disabled := loadCodeReachabilityProjectionIdleTuning(activationEnv(map[string]string{
		codeReachabilityProjectionMaxPollIntervalEnv: "45s",
		codeReachabilityProjectionAuditIntervalEnv:   "5m",
		codeReachabilityProjectionIdleProbeEnv:       "false",
	}))
	if maxPoll != 45*time.Second {
		t.Fatalf("maxPollInterval = %v, want 45s", maxPoll)
	}
	if audit != 5*time.Minute {
		t.Fatalf("auditInterval = %v, want 5m", audit)
	}
	if !disabled {
		t.Fatal("idleProbeDisabled = false, want true (kill-switch false disables the probe)")
	}

	maxPoll, audit, disabled = loadCodeReachabilityProjectionIdleTuning(activationEnv(map[string]string{
		codeReachabilityProjectionMaxPollIntervalEnv: "not-a-duration",
		codeReachabilityProjectionAuditIntervalEnv:   "-3m",
		codeReachabilityProjectionIdleProbeEnv:       "maybe",
	}))
	if maxPoll != 30*time.Second {
		t.Fatalf("maxPollInterval = %v, want 30s fallback", maxPoll)
	}
	if audit != 15*time.Minute {
		t.Fatalf("auditInterval = %v, want 15m fallback", audit)
	}
	if disabled {
		t.Fatal("idleProbeDisabled = true, want false fallback (probe stays enabled)")
	}
}

// TestCodeReachabilityProjectionRunnerWiresIdleTuning proves the For
// function carries the shared poll config plus the #10870 idle knobs and
// instruments into the runner.
func TestCodeReachabilityProjectionRunnerWiresIdleTuning(t *testing.T) {
	t.Parallel()

	db := activationWiringDB{t: t}
	sharedCfg := worker.RunnerConfig{
		PollInterval: 500 * time.Millisecond,
		BatchLimit:   10,
	}
	runner := codeReachabilityProjectionRunnerFor(db, sharedCfg, 4,
		activationEnv(map[string]string{
			codeReachabilityProjectionMaxPollIntervalEnv: "45s",
			codeReachabilityProjectionAuditIntervalEnv:   "5m",
		}),
		&telemetry.Instruments{},
		nil)
	if runner == nil {
		t.Fatal("codeReachabilityProjectionRunnerFor returned nil")
	}
	if got := runner.Config.PollInterval; got != 500*time.Millisecond {
		t.Fatalf("PollInterval = %v, want the shared 500ms", got)
	}
	if got := runner.Config.MaxPollInterval; got != 45*time.Second {
		t.Fatalf("MaxPollInterval = %v, want 45s", got)
	}
	if got := runner.Config.AuditInterval; got != 5*time.Minute {
		t.Fatalf("AuditInterval = %v, want 5m", got)
	}
	if runner.Config.DisableIdleProbe {
		t.Fatal("DisableIdleProbe = true, want false by default")
	}
	if runner.Instruments == nil {
		t.Fatal("Instruments = nil, want the shared instruments")
	}
	if runner.InputLoader == nil || runner.RowWriter == nil {
		t.Fatal("InputLoader/RowWriter must both be the Postgres store")
	}
}
