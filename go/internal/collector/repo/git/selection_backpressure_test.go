// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestLoadSelectionBackpressureConfigDisabledByDefault proves the zero
// value: no ESHU_GIT_SELECTION_* variable set leaves every-cycle
// selection untouched.
func TestLoadSelectionBackpressureConfigDisabledByDefault(t *testing.T) {
	t.Parallel()

	config, err := LoadSelectionBackpressureConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("LoadSelectionBackpressureConfig() error = %v, want nil", err)
	}
	if config.Enabled() {
		t.Fatalf("config = %+v, want disabled zero value", config)
	}
}

// TestLoadSelectionBackpressureConfigDefaultsMultiplier proves the
// proposed x4 stretch applies when the operator sets only the base
// interval, and an explicit multiplier wins.
func TestLoadSelectionBackpressureConfigDefaultsMultiplier(t *testing.T) {
	t.Parallel()

	env := map[string]string{GitSelectionMinResyncIntervalEnv: "1h"}
	config, err := LoadSelectionBackpressureConfig(func(name string) string { return env[name] })
	if err != nil {
		t.Fatalf("LoadSelectionBackpressureConfig() error = %v, want nil", err)
	}
	if config.StretchMultiplier != 4 {
		t.Fatalf("multiplier = %v, want proposed default 4", config.StretchMultiplier)
	}

	env[GitSelectionStretchMultiplierEnv] = "2"
	config, err = LoadSelectionBackpressureConfig(func(name string) string { return env[name] })
	if err != nil {
		t.Fatalf("LoadSelectionBackpressureConfig() error = %v, want nil", err)
	}
	if config.StretchMultiplier != 2 {
		t.Fatalf("multiplier = %v, want explicit 2", config.StretchMultiplier)
	}
}

// TestLoadSelectionBackpressureConfigRejectsOrphans proves a multiplier
// without a base interval, and a sub-unity multiplier, fail startup
// instead of running an undefined schedule.
func TestLoadSelectionBackpressureConfigRejectsOrphans(t *testing.T) {
	t.Parallel()

	if _, err := LoadSelectionBackpressureConfig(func(name string) string {
		if name == GitSelectionStretchMultiplierEnv {
			return "4"
		}
		return ""
	}); err == nil {
		t.Fatal("multiplier without base interval loads, want startup error")
	}
	if _, err := LoadSelectionBackpressureConfig(func(name string) string {
		if name == GitSelectionMinResyncIntervalEnv {
			return "1h"
		}
		if name == GitSelectionStretchMultiplierEnv {
			return "0"
		}
		return ""
	}); err == nil {
		t.Fatal("sub-unity multiplier loads, want startup error")
	}
}

// TestApplySelectionBackpressureStretchesWhileSlowed proves the AC3
// schedule leg: with the git source slowed, repos visited inside the
// stretched interval are deferred (kept pending, visited next cycle)
// while due repos still select. Nothing is dropped: the deferred repo
// selects again once its interval elapses.
func TestApplySelectionBackpressureStretchesWhileSlowed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	selector := &NativeRepositorySelector{
		Config:                RepoSyncConfig{SourceMode: "githubOrg"},
		Now:                   func() time.Time { return now },
		SelectionBackpressure: SelectionBackpressureConfig{MinResyncInterval: time.Hour, StretchMultiplier: 4},
		SelectionSlowed: func(context.Context) (bool, error) {
			return true, nil
		},
		selectionLastVisit: map[string]time.Time{
			"recent-repo": now.Add(-30 * time.Minute),
			"due-repo":    now.Add(-5 * time.Hour),
		},
	}

	selected := selector.applySelectionBackpressure(context.Background(), []string{"recent-repo", "due-repo", "new-repo"}, now)
	if len(selected) != 2 {
		t.Fatalf("selected = %v, want [due-repo new-repo]", selected)
	}
	// Deferred, never dropped: the recent repo stays tracked and
	// selects again once the stretched interval elapses.
	if _, ok := selector.selectionLastVisit["recent-repo"]; !ok {
		t.Fatal("deferred repo lost its visit stamp, want deferred-not-dropped")
	}
	later := now.Add(4 * time.Hour)
	selected = selector.applySelectionBackpressure(context.Background(), []string{"recent-repo"}, later)
	if len(selected) != 1 {
		t.Fatalf("selected after release window = %v, want [recent-repo]", selected)
	}
}

// TestApplySelectionBackpressureHealthyUsesBaseInterval proves the
// minimum resync interval still applies when the source is healthy, at
// the unstretched base: only the multiplier is the slowed-state effect.
func TestApplySelectionBackpressureHealthyUsesBaseInterval(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	selector := &NativeRepositorySelector{
		Config:                RepoSyncConfig{SourceMode: "githubOrg"},
		Now:                   func() time.Time { return now },
		SelectionBackpressure: SelectionBackpressureConfig{MinResyncInterval: time.Hour, StretchMultiplier: 4},
		SelectionSlowed: func(context.Context) (bool, error) {
			return false, nil
		},
		selectionLastVisit: map[string]time.Time{
			"recent-repo": now.Add(-30 * time.Minute),
			"hour-old":    now.Add(-90 * time.Minute),
		},
	}

	selected := selector.applySelectionBackpressure(context.Background(), []string{"recent-repo", "hour-old"}, now)
	if len(selected) != 1 || selected[0] != "hour-old" {
		t.Fatalf("selected = %v, want [hour-old]", selected)
	}
}

// TestApplySelectionBackpressureFailOpenAndDisabled proves the two
// no-op directions: an unreadable latch never wedges selection, and an
// unconfigured leg tracks nothing.
func TestApplySelectionBackpressureFailOpenAndDisabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	failing := &NativeRepositorySelector{
		Config:                RepoSyncConfig{SourceMode: "githubOrg"},
		Now:                   func() time.Time { return now },
		SelectionBackpressure: SelectionBackpressureConfig{MinResyncInterval: time.Hour, StretchMultiplier: 4},
		SelectionSlowed: func(context.Context) (bool, error) {
			return true, errTestSelectionBackpressure
		},
	}
	if selected := failing.applySelectionBackpressure(context.Background(), []string{"a"}, now); len(selected) != 1 {
		t.Fatalf("failing consult selected = %v, want [a] (fail open)", selected)
	}

	disabled := &NativeRepositorySelector{
		Config: RepoSyncConfig{SourceMode: "githubOrg"},
		Now:    func() time.Time { return now },
	}
	if selected := disabled.applySelectionBackpressure(context.Background(), []string{"a"}, now); len(selected) != 1 {
		t.Fatalf("disabled leg selected = %v, want [a]", selected)
	}
	if disabled.selectionLastVisit != nil {
		t.Fatal("disabled leg tracks visits, want zero behavior change")
	}
}

// scriptedSlowedStore answers the schedule-leg consult with one slowed
// verdict and records the source it was asked about.
type scriptedSlowedStore struct {
	slowed      bool
	askedSource string
	askedConfig workflow.SourceBackpressureConfig
}

func (s *scriptedSlowedStore) CheckSourceBackpressureForSource(
	_ context.Context,
	source string,
	config workflow.SourceBackpressureConfig,
	_ time.Time,
) (workflow.SourceBackpressureDecision, error) {
	s.askedSource = source
	s.askedConfig = config
	return workflow.SourceBackpressureDecision{SourceSystem: source, Slowed: s.slowed}, nil
}

func fullBackpressureEnv() map[string]string {
	return map[string]string{
		GitSelectionMinResyncIntervalEnv:             "1h",
		collector.SourceBackpressureEngageAgeEnv:     "1h",
		collector.SourceBackpressureReleaseAgeEnv:    "15m",
		collector.SourceBackpressureDepthFloorEnv:    "25",
		collector.SourceBackpressureGrantDelayEnv:    "2s",
		collector.SourceBackpressureMaxEngagementEnv: "30m",
	}
}

// TestWireSelectionBackpressureArmsConsult proves wiring: a full set arms
// the interval and a consult that delegates to the git source verdict,
// while an empty env leaves the selector untouched.
func TestWireSelectionBackpressureArmsConsult(t *testing.T) {
	t.Parallel()

	selector := &NativeRepositorySelector{}
	store := &scriptedSlowedStore{slowed: true}
	if err := selector.WireSelectionBackpressure(store, func(string) string { return "" }); err != nil {
		t.Fatalf("WireSelectionBackpressure() error = %v, want nil", err)
	}
	if selector.SelectionBackpressure.Enabled() || selector.SelectionSlowed != nil {
		t.Fatal("disabled wire armed the leg, want untouched selector")
	}

	selector = &NativeRepositorySelector{}
	env := fullBackpressureEnv()
	if err := selector.WireSelectionBackpressure(store, func(name string) string { return env[name] }); err != nil {
		t.Fatalf("WireSelectionBackpressure() error = %v, want nil", err)
	}
	if !selector.SelectionBackpressure.Enabled() || selector.SelectionSlowed == nil {
		t.Fatal("full wire left the leg disarmed, want armed")
	}
	slowed, err := selector.SelectionSlowed(context.Background())
	if err != nil || !slowed {
		t.Fatalf("SelectionSlowed() = %v, %v, want true, nil", slowed, err)
	}
	if store.askedSource != "git" {
		t.Fatalf("consult source = %q, want git", store.askedSource)
	}
	if !store.askedConfig.Enabled() {
		t.Fatal("consult ran with disabled verdict config, want enabled")
	}
}

// TestWireSelectionBackpressureRefusesStretchWithoutVerdict proves the
// pairing gate: a stretch without slowed-verdict thresholds fails
// startup instead of scheduling against unevaluable state.
func TestWireSelectionBackpressureRefusesStretchWithoutVerdict(t *testing.T) {
	t.Parallel()

	selector := &NativeRepositorySelector{}
	env := map[string]string{GitSelectionMinResyncIntervalEnv: "1h"}
	if err := selector.WireSelectionBackpressure(&scriptedSlowedStore{}, func(name string) string { return env[name] }); err == nil {
		t.Fatal("stretch without verdict thresholds wired, want startup error")
	}
}

// TestApplySelectionBackpressureForceReindexBypasses proves a forced
// reindex run visits everything: the stretch never interferes with an
// operator-forced full.
func TestApplySelectionBackpressureForceReindexBypasses(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	selector := &NativeRepositorySelector{
		Config:                RepoSyncConfig{SourceMode: "githubOrg", ForceReindex: true},
		Now:                   func() time.Time { return now },
		SelectionBackpressure: SelectionBackpressureConfig{MinResyncInterval: time.Hour, StretchMultiplier: 4},
		SelectionSlowed: func(context.Context) (bool, error) {
			return true, nil
		},
		selectionLastVisit: map[string]time.Time{"recent-repo": now},
	}

	selected := selector.applySelectionBackpressure(context.Background(), []string{"recent-repo"}, now)
	if len(selected) != 1 {
		t.Fatalf("forced reindex selected = %v, want [recent-repo]", selected)
	}
}
