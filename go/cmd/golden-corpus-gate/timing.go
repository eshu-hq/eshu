// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// PhaseTimings is the observed per-phase wall-clock of one live pipeline run,
// emitted by scripts/verify-golden-corpus-gate.sh as phase-timings.json. It is
// the B-11 (#3804) macro counterpart to the B-2 micro benchstat gate: micro
// catches a per-function ns/op regression, macro catches a per-phase wall-clock
// regression that no single benchmark would surface.
type PhaseTimings struct {
	SchemaVersion string `json:"schema_version"`
	// Phases maps a phase name (e.g. "bootstrap", "drains") to its observed
	// wall-clock seconds for this run.
	Phases map[string]float64 `json:"phases"`
}

// PhaseBaseline is the committed per-phase baseline (testdata/golden/
// e2e-baseline.json). It is updated deliberately when an intentional perf change
// lands, with the same review bar as code — a baseline bump is a reviewed claim
// that the new timing is the expected normal, not a silent ratchet.
type PhaseBaseline struct {
	SchemaVersion string `json:"schema_version"`
	// BaselineID names the corpus + backend the numbers were captured against, so
	// a baseline cannot be silently compared against an incompatible run shape.
	BaselineID string `json:"baseline_id"`
	// RegressionBand is the default allowed fractional regression (0.15 = 15%).
	// A per-invocation flag can override it; see evaluatePhaseTimings.
	RegressionBand float64 `json:"regression_band"`
	// AbsoluteSlackSeconds is an additive allowance combined with RegressionBand
	// as a logical OR: a phase passes if it is within the band OR within
	// baseline+slack. This mirrors the reducer claim-latency contract's "1.10x OR
	// +60s" dual rule and absorbs integer-second timing jitter on small phases,
	// where a 1s tick can exceed a 15% band. It does not mask a real regression on
	// the larger phases, where the relative band dominates.
	AbsoluteSlackSeconds float64                       `json:"absolute_slack_seconds"`
	Phases               map[string]PhaseBaselineEntry `json:"phases"`
}

// PhaseBaselineEntry is the baseline for one phase.
type PhaseBaselineEntry struct {
	// BaselineSeconds is the expected normal wall-clock for the phase.
	BaselineSeconds float64 `json:"baseline_seconds"`
	// Gated marks whether a regression in this phase blocks the gate. Phases
	// dominated by a fixed cost (e.g. a constant collector settle sleep) are
	// recorded for visibility but not gated, because their wall-clock reflects a
	// configured constant, not pipeline work that can regress.
	Gated bool `json:"gated"`
	// Note is human-facing and not asserted.
	Note string `json:"note"`
}

// LoadPhaseTimings reads the orchestrator-emitted observed timings.
func LoadPhaseTimings(path string) (PhaseTimings, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PhaseTimings{}, fmt.Errorf("read phase timings %q: %w", path, err)
	}
	var pt PhaseTimings
	if err := json.Unmarshal(raw, &pt); err != nil {
		return PhaseTimings{}, fmt.Errorf("parse phase timings %q: %w", path, err)
	}
	if len(pt.Phases) == 0 {
		return PhaseTimings{}, fmt.Errorf("phase timings %q has no phases", path)
	}
	return pt, nil
}

// LoadPhaseBaseline reads the committed per-phase baseline.
func LoadPhaseBaseline(path string) (PhaseBaseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PhaseBaseline{}, fmt.Errorf("read phase baseline %q: %w", path, err)
	}
	var pb PhaseBaseline
	if err := json.Unmarshal(raw, &pb); err != nil {
		return PhaseBaseline{}, fmt.Errorf("parse phase baseline %q: %w", path, err)
	}
	if len(pb.Phases) == 0 {
		return PhaseBaseline{}, fmt.Errorf("phase baseline %q has no phases", path)
	}
	if pb.RegressionBand <= 0 {
		return PhaseBaseline{}, fmt.Errorf("phase baseline %q has non-positive regression_band %g", path, pb.RegressionBand)
	}
	return pb, nil
}

// evaluatePhaseTimings produces one finding per baseline phase, asserting the
// observed wall-clock stayed within baseline × (1 + band). Gated phases are
// required (a regression fails the gate); non-gated phases are advisory. An
// observed phase missing from the run is a required failure for a gated phase —
// a gate that cannot measure a phase has not proven that phase did not regress.
//
// bandOverride, when > 0, replaces the baseline's RegressionBand (so a controlled
// run can tighten or loosen the band without editing the committed baseline).
//
// advisory downgrades every phase finding to non-blocking. Per-phase wall-clock
// is only a sound blocking signal when the run and the baseline share hardware:
// the committed baseline is captured on the controlled validation host, where the
// check is authoritative. On GitHub's shared CI runners, hardware varies more
// than the regression band between runs, so the golden-corpus-gate workflow runs
// this in advisory mode — the per-PR diff stays visible without false reds, the
// same reasoning behind the 2x total-wall-time budget multiplier.
func evaluatePhaseTimings(observed PhaseTimings, baseline PhaseBaseline, bandOverride float64, advisory bool, r *Report) {
	band := baseline.RegressionBand
	if bandOverride > 0 {
		band = bandOverride
	}
	// gatedAs reports the blocking tier for a phase: never blocking in advisory
	// mode; otherwise driven by the phase's own Gated flag.
	gatedAs := func(entry PhaseBaselineEntry) bool {
		return !advisory && entry.Gated
	}

	// Deterministic order so CI logs and tests are stable.
	names := make([]string, 0, len(baseline.Phases))
	for name := range baseline.Phases {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry := baseline.Phases[name]
		// Effective ceiling is the looser of the relative band and the absolute
		// slack, so small phases are not gated by integer-second jitter.
		ceiling := entry.BaselineSeconds * (1 + band)
		if slackCeiling := entry.BaselineSeconds + baseline.AbsoluteSlackSeconds; slackCeiling > ceiling {
			ceiling = slackCeiling
		}
		observedSecs, ok := observed.Phases[name]
		if !ok {
			r.AddCheck("timing", "phase_"+name, false, gatedAs(entry),
				fmt.Sprintf("no observed timing for phase %q (baseline=%.1fs)", name, entry.BaselineSeconds))
			continue
		}
		r.AddCheck("timing", "phase_"+name, observedSecs <= ceiling, gatedAs(entry),
			fmt.Sprintf("observed=%.1fs, baseline=%.1fs, ceiling=%.1fs (%.0f%% band or +%.0fs%s)",
				observedSecs, entry.BaselineSeconds, ceiling, band*100, baseline.AbsoluteSlackSeconds,
				gatedSuffix(gatedAs(entry))))
	}

	// Surface any observed phase with no baseline entry so a newly added pipeline
	// phase cannot slip in untracked. Advisory: a missing baseline entry is a
	// "add a baseline" prompt, not a regression.
	extra := make([]string, 0)
	for name := range observed.Phases {
		if _, known := baseline.Phases[name]; !known {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		r.AddCheck("timing", "phase_"+name+"_unbaselined", false, false,
			fmt.Sprintf("observed phase %q (%.1fs) has no baseline entry; add one to e2e-baseline.json", name, observed.Phases[name]))
	}
}

// gatedSuffix annotates advisory (non-gated) phase findings in the report detail.
func gatedSuffix(gated bool) string {
	if gated {
		return ""
	}
	return ", advisory"
}
