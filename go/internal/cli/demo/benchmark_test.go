// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrunbench"
)

// goodDemoEnvelope is a complete, passing run. Tests degrade one field at a
// time from here so each assertion names exactly one cause.
func goodDemoEnvelope() Envelope {
	return Envelope{
		Data: Result{
			Project: "eshu-demo",
			Ready:   true,
			FirstAnswer: Answer{
				Question: "Which workload does the api-svc repository run in?",
				Answer:   "Workload api-svc (kind: service) is defined in repository api-svc.",
				Truth:    map[string]any{"level": "derived", "basis": "hybrid", "freshness": "fresh"},
			},
			PhaseMillis: map[string]int64{
				"preflight": 43, "build": 1204, "up": 203642, "ready": 54617, "first_answer": 83,
			},
			TotalMillis: 259589,
		},
		Truth: map[string]any{"level": "derived", "basis": "hybrid", "freshness": "fresh"},
	}
}

func warmMeasurements() BenchmarkMeasurements {
	return BenchmarkMeasurements{
		Mode:           ModeWarm,
		Target:         6 * time.Minute,
		ImagesObserved: ImagesPresent,
	}
}

// An empty --mode must render a mode-shaped placeholder. The helper was
// copied from cmd/eshu's first_run.go, where it fills a REPO argument slot,
// and the copy kept that placeholder; in this package the mode line is the
// only caller, so `mode : <repo>` was a repository placeholder in a field
// that holds cold/warm (#6152 review).
func TestRenderBenchmarkVerdict_EmptyModeRendersUnsetPlaceholder(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	RenderBenchmarkVerdict(&out, BenchmarkVerdict{Mode: ""})
	if !strings.Contains(out.String(), "mode : <unset>\n") {
		t.Fatalf("output = %q, want the empty mode rendered as <unset>", out.String())
	}
	if strings.Contains(out.String(), "<repo>") {
		t.Fatalf("output = %q, must not use the repo placeholder for a mode", out.String())
	}
}

func TestEvaluateDemoBenchmark_PassesACompleteWarmRun(t *testing.T) {
	t.Parallel()
	v := EvaluateBenchmark(goodDemoEnvelope(), warmMeasurements())
	if !v.Pass {
		t.Fatalf("Pass = false, want true; reasons: %v", v.FailureReasons())
	}
	if v.Mode != ModeWarm {
		t.Errorf("Mode = %q, want %q", v.Mode, ModeWarm)
	}
	if got := v.Criterion(firstrunbench.CriterionTimeToAnswer); got.Status != firstrunbench.CriterionPass {
		t.Errorf("time_to_first_answer = %q (%s), want pass", got.Status, got.Detail)
	}
}

// TestEvaluateDemoBenchmark_FailsOnMissingPhaseTiming is acceptance criterion 3:
// an envelope missing phase timings must fail the verdict rather than score a
// total whose composition cannot be checked.
func TestEvaluateDemoBenchmark_FailsOnMissingPhaseTiming(t *testing.T) {
	t.Parallel()
	for _, phase := range requiredPhases {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			env := goodDemoEnvelope()
			delete(env.Data.PhaseMillis, phase)
			v := EvaluateBenchmark(env, warmMeasurements())
			if v.Pass {
				t.Fatalf("Pass = true with %q missing, want false", phase)
			}
			c := v.Criterion(CriterionPhaseTimings)
			if c.Status != firstrunbench.CriterionFail {
				t.Errorf("phase_timings_complete = %q, want fail", c.Status)
			}
			if !strings.Contains(c.Detail, phase) {
				t.Errorf("detail = %q, want it to name the missing phase %q", c.Detail, phase)
			}
		})
	}
}

// TestEvaluateDemoBenchmark_FailsWhenOverTarget is the other half of acceptance
// criterion 3.
func TestEvaluateDemoBenchmark_FailsWhenOverTarget(t *testing.T) {
	t.Parallel()
	env := goodDemoEnvelope()
	m := warmMeasurements()
	m.Target = time.Duration(env.Data.TotalMillis-1) * time.Millisecond

	v := EvaluateBenchmark(env, m)
	if v.Pass {
		t.Fatal("Pass = true for a run over target, want false")
	}
	if c := v.Criterion(firstrunbench.CriterionTimeToAnswer); c.Status != firstrunbench.CriterionFail {
		t.Errorf("time_to_first_answer = %q (%s), want fail", c.Status, c.Detail)
	}
}

// TestEvaluateDemoBenchmark_RejectsAMislabelledMode is why the mode is
// cross-checked rather than trusted. A warm run published as COLD understates
// the number a first-time installer actually experiences, and a declared label
// alone cannot catch it.
func TestEvaluateDemoBenchmark_RejectsAMislabelledMode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		mode     string
		observed ImageState
		wantPass bool
	}{
		{"warm declared, images present", ModeWarm, ImagesPresent, true},
		{"cold declared, images absent", ModeCold, ImagesAbsent, true},
		{"cold declared but images present", ModeCold, ImagesPresent, false},
		{"warm declared but images absent", ModeWarm, ImagesAbsent, false},
		// The not-probed fallback is the only evaluateDemoModeCriterion branch
		// the four permutations above miss. It must pass without claiming a
		// check happened, so it records not_measured and stops being required.
		{"cache not probed, cold", ModeCold, ImagesUnknown, true},
		{"cache not probed, warm", ModeWarm, ImagesUnknown, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := warmMeasurements()
			m.Mode, m.ImagesObserved = tc.mode, tc.observed
			v := EvaluateBenchmark(goodDemoEnvelope(), m)
			if v.Pass != tc.wantPass {
				t.Errorf("Pass = %v, want %v (%s)", v.Pass, tc.wantPass,
					v.Criterion(CriterionModeObserved).Detail)
			}
			c := v.Criterion(CriterionModeObserved)
			if tc.observed == ImagesUnknown {
				// Not probed must read as not-measured and drop its required
				// flag, so the row never implies a check that did not run.
				if c.Status != firstrunbench.CriterionNotMeasured || c.Required {
					t.Errorf("unprobed cache scored %q required=%v, want not_measured and not required",
						c.Status, c.Required)
				}
				if !strings.Contains(c.Detail, "not probed") {
					t.Errorf("detail = %q, want it to say the cache was not probed", c.Detail)
				}
			} else if c.Status == firstrunbench.CriterionNotMeasured {
				t.Errorf("probed cache scored not_measured; the cross-check was skipped")
			}
		})
	}
}

// TestEvaluateDemoBenchmark_RejectsAHealthOnlyRun carries the first-run
// benchmark's load-bearing invariant across to the demo: a stack that came up
// but never answered is not a first answer.
func TestEvaluateDemoBenchmark_RejectsAHealthOnlyRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		mutate  func(*Envelope)
		wantRow firstrunbench.CriterionName
	}{
		{"no answer text", func(e *Envelope) { e.Data.FirstAnswer.Answer = "" }, firstrunbench.CriterionFirstAnswer},
		{"not ready", func(e *Envelope) { e.Data.Ready = false }, firstrunbench.CriterionRepoIndexed},
		{"no truth labels", func(e *Envelope) {
			e.Truth = map[string]any{}
			e.Data.FirstAnswer.Truth = map[string]any{}
		}, firstrunbench.CriterionTruthMetadata},
		{"envelope error", func(e *Envelope) {
			e.Error = &firstrunbench.EnvelopeError{Message: "compose failed"}
		}, firstrunbench.CriterionFirstAnswer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := goodDemoEnvelope()
			tc.mutate(&env)
			v := EvaluateBenchmark(env, warmMeasurements())
			if v.Pass {
				t.Fatalf("Pass = true for %q, want false", tc.name)
			}
			if c := v.Criterion(tc.wantRow); c.Status != firstrunbench.CriterionFail {
				t.Errorf("%s = %q, want fail", tc.wantRow, c.Status)
			}
		})
	}
}

// TestEvaluateDemoBenchmark_ReportsColdAndWarmSeparately is acceptance
// criterion 1: the two modes carry their own targets, and a blended number is
// never produced.
func TestEvaluateDemoBenchmark_ReportsColdAndWarmSeparately(t *testing.T) {
	t.Parallel()
	env := goodDemoEnvelope()

	warm := warmMeasurements()
	cold := BenchmarkMeasurements{
		Mode: ModeCold, Target: 9 * time.Minute, ImagesObserved: ImagesAbsent,
	}
	if got := EvaluateBenchmark(env, warm); got.Mode != ModeWarm || got.TargetMillis != warm.Target.Milliseconds() {
		t.Errorf("warm verdict = mode %q target %d, want %q / %d",
			got.Mode, got.TargetMillis, ModeWarm, warm.Target.Milliseconds())
	}
	if got := EvaluateBenchmark(env, cold); got.Mode != ModeCold || got.TargetMillis != cold.Target.Milliseconds() {
		t.Errorf("cold verdict = mode %q target %d, want %q / %d",
			got.Mode, got.TargetMillis, ModeCold, cold.Target.Milliseconds())
	}
}
