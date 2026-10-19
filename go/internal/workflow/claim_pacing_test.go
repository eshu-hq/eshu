// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"errors"
	"testing"
	"time"
)

// testPacingConfig returns a valid pacing config with small thresholds the
// pacing proofs share.
func testPacingConfig() ClaimPacingConfig {
	return ClaimPacingConfig{
		DomainHigh:    100,
		DomainLow:     40,
		GlobalHigh:    1000,
		GlobalLow:     400,
		GrantDelay:    2 * time.Second,
		MaxEngagement: time.Minute,
		DepthCacheTTL: time.Second,
	}
}

// TestClaimPacingConfigValidate pins the config contract: zero disables,
// enabled configs need strictly ordered watermarks and positive delays.
func TestClaimPacingConfigValidate(t *testing.T) {
	t.Parallel()

	if cfg := (ClaimPacingConfig{}); cfg.Enabled() {
		t.Fatal("zero ClaimPacingConfig Enabled() = true, want false (pacing off unless configured)")
	}
	if err := (ClaimPacingConfig{}).Validate(); err != nil {
		t.Fatalf("zero ClaimPacingConfig Validate() error = %v, want nil (disabled is valid)", err)
	}
	cfg := testPacingConfig()
	if !cfg.Enabled() {
		t.Fatal("testPacingConfig Enabled() = false, want true")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	cases := map[string]func(*ClaimPacingConfig){
		"domain low equals high": func(c *ClaimPacingConfig) { c.DomainLow = c.DomainHigh },
		"domain low above high":  func(c *ClaimPacingConfig) { c.DomainLow = c.DomainHigh + 1 },
		"global low above high":  func(c *ClaimPacingConfig) { c.GlobalLow = c.GlobalHigh + 1 },
		"zero grant delay":       func(c *ClaimPacingConfig) { c.GrantDelay = 0 },
		"negative grant delay":   func(c *ClaimPacingConfig) { c.GrantDelay = -time.Second },
		"zero max engagement":    func(c *ClaimPacingConfig) { c.MaxEngagement = 0 },
		"zero domain high":       func(c *ClaimPacingConfig) { c.DomainHigh = 0 },
		"zero global high":       func(c *ClaimPacingConfig) { c.GlobalHigh = 0 },
	}
	for name, mutate := range cases {
		mutated := testPacingConfig()
		mutate(&mutated)
		if err := mutated.Validate(); err == nil {
			t.Errorf("Validate() with %s = nil, want an error", name)
		}
	}
}

// TestDecideClaimPacingEngagesOnDomainDepthFirst proves the signal order the
// issue requires: per-domain depth engages before the global ceiling is
// consulted, and the verdict names the domain signal with its domain.
func TestDecideClaimPacingEngagesOnDomainDepthFirst(t *testing.T) {
	t.Parallel()
	cfg := testPacingConfig()

	got := DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 101, Global: 10}, ClaimPacingLatch{}, 0)
	if !got.Paced {
		t.Fatal("DecideClaimPacing over domain high Paced = false, want true")
	}
	if got.Signal != ClaimPacingSignalDomain {
		t.Fatalf("Signal = %q, want %q", got.Signal, ClaimPacingSignalDomain)
	}
	if got.RetryAfter != cfg.GrantDelay {
		t.Fatalf("RetryAfter = %s, want grant delay %s", got.RetryAfter, cfg.GrantDelay)
	}
	if got.Stuck {
		t.Fatal("fresh engagement Stuck = true, want false")
	}

	got = DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 10, Global: 1001}, ClaimPacingLatch{}, 0)
	if !got.Paced || got.Signal != ClaimPacingSignalGlobal {
		t.Fatalf("global-only breach = %+v, want paced on global", got)
	}

	got = DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 101, Global: 1001}, ClaimPacingLatch{}, 0)
	if !got.Paced || got.Signal != ClaimPacingSignalDomain {
		t.Fatalf("both breached = %+v, want paced on domain first", got)
	}

	got = DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 100, Global: 1000}, ClaimPacingLatch{}, 0)
	if got.Paced {
		t.Fatalf("depths exactly at high watermarks = %+v, want unpaced (engage strictly above)", got)
	}
}

// TestDecideClaimPacingHysteresis proves the latch: between low and high the
// verdict follows prior engagement, and release needs the engaged signal at
// or below its own low watermark.
func TestDecideClaimPacingHysteresis(t *testing.T) {
	t.Parallel()
	cfg := testPacingConfig()
	mid := ClaimPacingDepths{Domain: 70, Global: 10}

	disengaged := DecideClaimPacing(cfg, mid, ClaimPacingLatch{}, 0)
	if disengaged.Paced {
		t.Fatalf("mid-band disengaged = %+v, want unpaced", disengaged)
	}
	engaged := DecideClaimPacing(cfg, mid, ClaimPacingLatch{Engaged: true, Signal: ClaimPacingSignalDomain}, 0)
	if !engaged.Paced || engaged.Signal != ClaimPacingSignalDomain {
		t.Fatalf("mid-band engaged = %+v, want still paced on domain", engaged)
	}

	atLow := DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 40, Global: 10},
		ClaimPacingLatch{Engaged: true, Signal: ClaimPacingSignalDomain}, 0)
	if atLow.Paced {
		t.Fatalf("domain at low watermark = %+v, want released", atLow)
	}
	aboveLow := DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 41, Global: 10},
		ClaimPacingLatch{Engaged: true, Signal: ClaimPacingSignalDomain}, 0)
	if !aboveLow.Paced {
		t.Fatalf("domain just above low = %+v, want still paced", aboveLow)
	}

	globalEngaged := ClaimPacingLatch{Engaged: true, Signal: ClaimPacingSignalGlobal}
	globalMid := DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 10, Global: 700}, globalEngaged, 0)
	if !globalMid.Paced || globalMid.Signal != ClaimPacingSignalGlobal {
		t.Fatalf("global mid-band engaged = %+v, want still paced on global", globalMid)
	}
	globalReleased := DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 10, Global: 400}, globalEngaged, 0)
	if globalReleased.Paced {
		t.Fatalf("global at low watermark = %+v, want released", globalReleased)
	}
}

// TestDecideClaimPacingSignalSwitch proves an engaged latch follows a newly
// hot signal instead of releasing into a saturated system: domain releases
// while global is hot re-engages on global, and vice versa.
func TestDecideClaimPacingSignalSwitch(t *testing.T) {
	t.Parallel()
	cfg := testPacingConfig()

	got := DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 10, Global: 1001},
		ClaimPacingLatch{Engaged: true, Signal: ClaimPacingSignalDomain}, 0)
	if !got.Paced || got.Signal != ClaimPacingSignalGlobal {
		t.Fatalf("domain released into hot global = %+v, want paced on global", got)
	}
	got = DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 101, Global: 10},
		ClaimPacingLatch{Engaged: true, Signal: ClaimPacingSignalGlobal}, 0)
	if !got.Paced || got.Signal != ClaimPacingSignalDomain {
		t.Fatalf("global released into hot domain = %+v, want paced on domain", got)
	}
}

// TestDecideClaimPacingStuck proves the stuck-pacing signal: an engagement
// older than MaxEngagement with depth still above the release watermark stays
// paced and goes loud instead of releasing blindly.
func TestDecideClaimPacingStuck(t *testing.T) {
	t.Parallel()
	cfg := testPacingConfig()
	latch := ClaimPacingLatch{Engaged: true, Signal: ClaimPacingSignalDomain}

	fresh := DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 200, Global: 10}, latch, 30*time.Second)
	if !fresh.Paced || fresh.Stuck {
		t.Fatalf("fresh engagement = %+v, want paced without stuck", fresh)
	}
	stuck := DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 200, Global: 10}, latch, 61*time.Second)
	if !stuck.Paced || !stuck.Stuck {
		t.Fatalf("overdue engagement = %+v, want paced with stuck", stuck)
	}
	if stuck.RetryAfter != cfg.GrantDelay {
		t.Fatalf("stuck RetryAfter = %s, want grant delay %s", stuck.RetryAfter, cfg.GrantDelay)
	}
	released := DecideClaimPacing(cfg, ClaimPacingDepths{Domain: 10, Global: 10}, latch, 61*time.Second)
	if released.Paced || released.Stuck {
		t.Fatalf("drained overdue engagement = %+v, want released without stuck", released)
	}
}

// TestDecideClaimPacingDisabled proves a zero config never paces, whatever
// the depths: pacing is opt-in by configuration.
func TestDecideClaimPacingDisabled(t *testing.T) {
	t.Parallel()

	got := DecideClaimPacing(ClaimPacingConfig{}, ClaimPacingDepths{Domain: 1 << 30, Global: 1 << 30}, ClaimPacingLatch{}, 0)
	if got.Paced {
		t.Fatalf("disabled config = %+v, want unpaced", got)
	}
}

// TestClaimPacingDeferralCarriesRetryAfter proves the grant-delay channel:
// the deferral error exposes the existing retry-after delay so the runner
// backs off without treating a paced grant as a failure.
func TestClaimPacingDeferralCarriesRetryAfter(t *testing.T) {
	t.Parallel()

	deferral := ClaimPacingDeferral{
		Signal:     ClaimPacingSignalDomain,
		Domain:     "git/git-primary",
		RetryAfter: 2 * time.Second,
		Stuck:      true,
	}
	var retryAfter interface{ RetryAfterDelay() time.Duration }
	if !errors.As(error(deferral), &retryAfter) {
		t.Fatal("ClaimPacingDeferral does not expose RetryAfterDelay")
	}
	if got := retryAfter.RetryAfterDelay(); got != 2*time.Second {
		t.Fatalf("RetryAfterDelay() = %s, want 2s", got)
	}
	if deferral.Error() == "" {
		t.Fatal("Error() is empty, want a diagnosable message")
	}
}
