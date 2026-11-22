// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"testing"
	"time"
)

// testSourceBackpressureConfig returns a valid per-source backpressure config
// with small thresholds the slowed-state proofs share. The values mirror the
// Proposed S0/S1 numbers from the #10882 design record; they are test-only
// until measurement confirms them.
func testSourceBackpressureConfig() SourceBackpressureConfig {
	return SourceBackpressureConfig{
		EngageAge:     time.Hour,
		ReleaseAge:    15 * time.Minute,
		DepthFloor:    25,
		GrantDelay:    2 * time.Second,
		MaxEngagement: 30 * time.Minute,
	}
}

// rowsAt builds the observation for depth claimable rows that all sit at age,
// so the engage and release row counts follow the configured ages.
func rowsAt(cfg SourceBackpressureConfig, source string, age time.Duration, depth int64) SourceQueueSignal {
	return signalFromRows(cfg, source, repeatAge(age, int(depth)))
}

// TestSourceBackpressureConfigValidate pins the config contract: the zero
// value disables the per-source leg, an enabled config needs a release age
// strictly below the engage age, a positive depth floor, and a positive
// engagement bound.
func TestSourceBackpressureConfigValidate(t *testing.T) {
	t.Parallel()

	if cfg := (SourceBackpressureConfig{}); cfg.Enabled() {
		t.Fatal("zero SourceBackpressureConfig Enabled() = true, want false (per-source leg off unless configured)")
	}
	if err := (SourceBackpressureConfig{}).Validate(); err != nil {
		t.Fatalf("zero SourceBackpressureConfig Validate() error = %v, want nil (disabled is valid)", err)
	}
	cfg := testSourceBackpressureConfig()
	if !cfg.Enabled() {
		t.Fatal("testSourceBackpressureConfig Enabled() = false, want true")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	cases := map[string]func(*SourceBackpressureConfig){
		"only engage age (partial)": func(c *SourceBackpressureConfig) {
			*c = SourceBackpressureConfig{EngageAge: time.Hour}
		},
		"release equals engage": func(c *SourceBackpressureConfig) { c.ReleaseAge = c.EngageAge },
		"release above engage":  func(c *SourceBackpressureConfig) { c.ReleaseAge = 2 * c.EngageAge },
		"zero release age":      func(c *SourceBackpressureConfig) { c.ReleaseAge = 0 },
		"zero depth floor":      func(c *SourceBackpressureConfig) { c.DepthFloor = 0 },
		"negative depth floor":  func(c *SourceBackpressureConfig) { c.DepthFloor = -1 },
		"zero grant delay":      func(c *SourceBackpressureConfig) { c.GrantDelay = 0 },
		"negative grant delay":  func(c *SourceBackpressureConfig) { c.GrantDelay = -time.Second },
		"zero max engagement":   func(c *SourceBackpressureConfig) { c.MaxEngagement = 0 },
		"negative engage age":   func(c *SourceBackpressureConfig) { c.EngageAge = -time.Hour },
	}
	for name, mutate := range cases {
		mutated := testSourceBackpressureConfig()
		mutate(&mutated)
		if err := mutated.Validate(); err == nil {
			t.Errorf("Validate() with %s = nil, want an error", name)
		}
	}
}

// TestDecideSourceSlowedEngagesOnAgeWithDepthFloor proves the trigger the
// #10882 design record names: a source slows only when its oldest claimable
// reducer age exceeds the engage age AND its claimable depth reaches the
// floor. Either signal alone stays unslowed.
func TestDecideSourceSlowedEngagesOnAgeWithDepthFloor(t *testing.T) {
	t.Parallel()
	cfg := testSourceBackpressureConfig()

	got := DecideSourceSlowed(cfg, rowsAt(cfg, "vuln-intel", 2*time.Hour, 25), SourceBackpressureLatch{}, 0)
	if !got.Slowed {
		t.Fatal("old and deep signal Slowed = false, want true")
	}
	if got.Reason != SourceBackpressureReasonSourceBehind {
		t.Fatalf("Reason = %q, want %q", got.Reason, SourceBackpressureReasonSourceBehind)
	}
	if got.SourceSystem != "vuln-intel" {
		t.Fatalf("SourceSystem = %q, want vuln-intel", got.SourceSystem)
	}
	if got.Stuck {
		t.Fatal("fresh engagement Stuck = true, want false")
	}

	oldShallow := DecideSourceSlowed(cfg, rowsAt(cfg, "vuln-intel", 2*time.Hour, 24), SourceBackpressureLatch{}, 0)
	if oldShallow.Slowed {
		t.Fatalf("old but shallow signal = %+v, want unslowed (single poison row must not throttle the source)", oldShallow)
	}

	youngDeep := DecideSourceSlowed(cfg, rowsAt(cfg, "vuln-intel", 30*time.Minute, 10000), SourceBackpressureLatch{}, 0)
	if youngDeep.Slowed {
		t.Fatalf("young but deep signal = %+v, want unslowed (fast-draining backlog is fresh)", youngDeep)
	}

	atEngage := DecideSourceSlowed(cfg, rowsAt(cfg, "vuln-intel", time.Hour, 25), SourceBackpressureLatch{}, 0)
	if atEngage.Slowed {
		t.Fatalf("age exactly at engage age = %+v, want unslowed (engage strictly above)", atEngage)
	}
}

// TestDecideSourceSlowedHysteresis proves the latch: between release and
// engage the verdict follows prior engagement, and release needs the oldest
// claimable age at or below the release age.
func TestDecideSourceSlowedHysteresis(t *testing.T) {
	t.Parallel()
	cfg := testSourceBackpressureConfig()
	mid := rowsAt(cfg, "git", 30*time.Minute, 100)

	disengaged := DecideSourceSlowed(cfg, mid, SourceBackpressureLatch{}, 0)
	if disengaged.Slowed {
		t.Fatalf("mid-band disengaged = %+v, want unslowed", disengaged)
	}
	engaged := DecideSourceSlowed(cfg, mid, SourceBackpressureLatch{Engaged: true}, 0)
	if !engaged.Slowed || engaged.Reason != SourceBackpressureReasonSourceBehind {
		t.Fatalf("mid-band engaged = %+v, want still slowed", engaged)
	}

	atRelease := DecideSourceSlowed(cfg, rowsAt(cfg, "git", 15*time.Minute, 100), SourceBackpressureLatch{Engaged: true}, 0)
	if atRelease.Slowed {
		t.Fatalf("age at release age = %+v, want released", atRelease)
	}
	aboveRelease := DecideSourceSlowed(cfg, rowsAt(cfg, "git", 15*time.Minute+time.Second, 100), SourceBackpressureLatch{Engaged: true}, 0)
	if !aboveRelease.Slowed {
		t.Fatalf("age just above release = %+v, want still slowed", aboveRelease)
	}
}

// TestDecideSourceSlowedStuck proves the stuck-slowing signal: an engagement
// older than MaxEngagement with age still above the release watermark stays
// slowed and goes loud instead of releasing blindly into a saturated source.
func TestDecideSourceSlowedStuck(t *testing.T) {
	t.Parallel()
	cfg := testSourceBackpressureConfig()
	latch := SourceBackpressureLatch{Engaged: true}
	signal := rowsAt(cfg, "git", 2*time.Hour, 100)

	fresh := DecideSourceSlowed(cfg, signal, latch, 29*time.Minute)
	if !fresh.Slowed || fresh.Stuck {
		t.Fatalf("fresh engagement = %+v, want slowed without stuck", fresh)
	}
	stuck := DecideSourceSlowed(cfg, signal, latch, 31*time.Minute)
	if !stuck.Slowed || !stuck.Stuck {
		t.Fatalf("overdue engagement = %+v, want slowed with stuck", stuck)
	}
	released := DecideSourceSlowed(cfg, rowsAt(cfg, "git", time.Minute, 100), latch, 31*time.Minute)
	if released.Slowed || released.Stuck {
		t.Fatalf("drained overdue engagement = %+v, want released without stuck", released)
	}
}

// TestDecideSourceSlowedDisabled proves a zero config never slows, whatever
// the signal: the per-source leg is opt-in by configuration.
func TestDecideSourceSlowedDisabled(t *testing.T) {
	t.Parallel()

	got := DecideSourceSlowed(SourceBackpressureConfig{}, SourceQueueSignal{
		SourceSystem:       "vuln-intel",
		OldestClaimableAge: 30 * 24 * time.Hour,
		ClaimableDepth:     1 << 30,
	}, SourceBackpressureLatch{}, 0)
	if got.Slowed {
		t.Fatalf("disabled config = %+v, want unslowed", got)
	}
}

// TestDecideSourceSlowedIsPerSource proves the fairness isolation AC5 needs:
// one source's backlog slows only that source, never a healthy sibling.
func TestDecideSourceSlowedIsPerSource(t *testing.T) {
	t.Parallel()
	cfg := testSourceBackpressureConfig()

	behind := DecideSourceSlowed(cfg, rowsAt(cfg, "vuln-intel", 25*24*time.Hour+19*time.Hour, 6363), SourceBackpressureLatch{}, 0)
	healthy := DecideSourceSlowed(cfg, rowsAt(cfg, "git", 5*time.Minute, 200), SourceBackpressureLatch{}, 0)
	if !behind.Slowed {
		t.Fatalf("behind source = %+v, want slowed", behind)
	}
	if healthy.Slowed {
		t.Fatalf("healthy source = %+v, want unslowed while a sibling is behind", healthy)
	}
	if behind.SourceSystem == healthy.SourceSystem {
		t.Fatal("verdicts do not name their source systems")
	}
}

// TestDecideSourceSlowedCarriesGrantDelay proves a slowed verdict carries the
// configured grant delay, so claim-grant and admission callers back off on the
// existing retry-after path instead of busy-polling the consult.
func TestDecideSourceSlowedCarriesGrantDelay(t *testing.T) {
	t.Parallel()
	cfg := testSourceBackpressureConfig()

	got := DecideSourceSlowed(cfg, rowsAt(cfg, "vuln-intel", 2*time.Hour, 100), SourceBackpressureLatch{}, 0)
	if !got.Slowed {
		t.Fatal("Slowed = false, want true")
	}
	if got.RetryAfter != cfg.GrantDelay {
		t.Fatalf("RetryAfter = %s, want grant delay %s", got.RetryAfter, cfg.GrantDelay)
	}

	clear := DecideSourceSlowed(cfg, SourceQueueSignal{
		SourceSystem: "vuln-intel",
	}, SourceBackpressureLatch{}, 0)
	if clear.RetryAfter != 0 {
		t.Fatalf("unslowed RetryAfter = %s, want 0", clear.RetryAfter)
	}
}

// TestClaimPacingSignalReducerSourceValidates proves the reducer-source
// pacing signal (#10882 decision 4) is a closed wire value alongside the
// domain and global signals, so latch rows and deferrals carry it without
// the unknown-signal widening.
func TestClaimPacingSignalReducerSourceValidates(t *testing.T) {
	t.Parallel()

	if err := ClaimPacingSignalReducerSource.Validate(); err != nil {
		t.Fatalf("ClaimPacingSignalReducerSource.Validate() error = %v, want nil", err)
	}
	if got := string(ClaimPacingSignalReducerSource); got != "reducer_source" {
		t.Fatalf("ClaimPacingSignalReducerSource = %q, want reducer_source", got)
	}
}
