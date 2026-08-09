// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goodDemoEnvelope is a complete, passing run. Tests degrade one field at a
// time from here so each assertion names exactly one cause.
func goodDemoEnvelope() demoEnvelope {
	return demoEnvelope{
		Data: demoResult{
			Project: "eshu-demo",
			Ready:   true,
			FirstAnswer: demoAnswer{
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

func warmMeasurements() demoBenchmarkMeasurements {
	return demoBenchmarkMeasurements{
		Mode:           demoModeWarm,
		Target:         6 * time.Minute,
		ImagesObserved: demoImagesPresent,
	}
}

func TestEvaluateDemoBenchmark_PassesACompleteWarmRun(t *testing.T) {
	t.Parallel()
	v := evaluateDemoBenchmark(goodDemoEnvelope(), warmMeasurements())
	if !v.Pass {
		t.Fatalf("Pass = false, want true; reasons: %v", v.failureReasons())
	}
	if v.Mode != demoModeWarm {
		t.Errorf("Mode = %q, want %q", v.Mode, demoModeWarm)
	}
	if got := v.criterion(criterionTimeToAnswer); got.Status != benchmarkCriterionPass {
		t.Errorf("time_to_first_answer = %q (%s), want pass", got.Status, got.Detail)
	}
}

// TestEvaluateDemoBenchmark_FailsOnMissingPhaseTiming is acceptance criterion 3:
// an envelope missing phase timings must fail the verdict rather than score a
// total whose composition cannot be checked.
func TestEvaluateDemoBenchmark_FailsOnMissingPhaseTiming(t *testing.T) {
	t.Parallel()
	for _, phase := range demoRequiredPhases {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			env := goodDemoEnvelope()
			delete(env.Data.PhaseMillis, phase)
			v := evaluateDemoBenchmark(env, warmMeasurements())
			if v.Pass {
				t.Fatalf("Pass = true with %q missing, want false", phase)
			}
			c := v.criterion(criterionDemoPhaseTimings)
			if c.Status != benchmarkCriterionFail {
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

	v := evaluateDemoBenchmark(env, m)
	if v.Pass {
		t.Fatal("Pass = true for a run over target, want false")
	}
	if c := v.criterion(criterionTimeToAnswer); c.Status != benchmarkCriterionFail {
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
		observed demoImageState
		wantPass bool
	}{
		{"warm declared, images present", demoModeWarm, demoImagesPresent, true},
		{"cold declared, images absent", demoModeCold, demoImagesAbsent, true},
		{"cold declared but images present", demoModeCold, demoImagesPresent, false},
		{"warm declared but images absent", demoModeWarm, demoImagesAbsent, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := warmMeasurements()
			m.Mode, m.ImagesObserved = tc.mode, tc.observed
			v := evaluateDemoBenchmark(goodDemoEnvelope(), m)
			if v.Pass != tc.wantPass {
				t.Errorf("Pass = %v, want %v (%s)", v.Pass, tc.wantPass,
					v.criterion(criterionDemoModeObserved).Detail)
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
		mutate  func(*demoEnvelope)
		wantRow benchmarkCriterionName
	}{
		{"no answer text", func(e *demoEnvelope) { e.Data.FirstAnswer.Answer = "" }, criterionFirstAnswer},
		{"not ready", func(e *demoEnvelope) { e.Data.Ready = false }, criterionRepoIndexed},
		{"no truth labels", func(e *demoEnvelope) {
			e.Truth = map[string]any{}
			e.Data.FirstAnswer.Truth = map[string]any{}
		}, criterionTruthMetadata},
		{"envelope error", func(e *demoEnvelope) {
			e.Error = &firstRunEnvelopeError{Message: "compose failed"}
		}, criterionFirstAnswer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := goodDemoEnvelope()
			tc.mutate(&env)
			v := evaluateDemoBenchmark(env, warmMeasurements())
			if v.Pass {
				t.Fatalf("Pass = true for %q, want false", tc.name)
			}
			if c := v.criterion(tc.wantRow); c.Status != benchmarkCriterionFail {
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
	cold := demoBenchmarkMeasurements{
		Mode: demoModeCold, Target: 9 * time.Minute, ImagesObserved: demoImagesAbsent,
	}
	if got := evaluateDemoBenchmark(env, warm); got.Mode != demoModeWarm || got.TargetMillis != warm.Target.Milliseconds() {
		t.Errorf("warm verdict = mode %q target %d, want %q / %d",
			got.Mode, got.TargetMillis, demoModeWarm, warm.Target.Milliseconds())
	}
	if got := evaluateDemoBenchmark(env, cold); got.Mode != demoModeCold || got.TargetMillis != cold.Target.Milliseconds() {
		t.Errorf("cold verdict = mode %q target %d, want %q / %d",
			got.Mode, got.TargetMillis, demoModeCold, cold.Target.Milliseconds())
	}
}

// TestDemoBenchmarkCommand_ExitsNonZeroOnFailure proves the verdict reaches the
// process boundary. A scorer that prints FAILED but exits 0 lets a measurement
// script record a missed target as a pass, which is the failure mode the whole
// lane exists to prevent.
func TestDemoBenchmarkCommand_ExitsNonZeroOnFailure(t *testing.T) {
	env := goodDemoEnvelope()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "demo.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"within target", []string{"--mode", "warm", "--images", "present", "--target", "10m"}, false},
		{"over target", []string{"--mode", "warm", "--images", "present", "--target", "1s"}, true},
		{"mislabelled cold", []string{"--mode", "cold", "--images", "present", "--target", "10m"}, true},
		{"bad images value", []string{"--mode", "warm", "--images", "sometimes"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newDemoBenchmarkCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(append([]string{"--envelope", path}, tc.args...))

			runErr := cmd.Execute()
			if tc.wantErr && runErr == nil {
				t.Fatalf("Execute() = nil, want an error; output:\n%s", out.String())
			}
			if !tc.wantErr && runErr != nil {
				t.Fatalf("Execute() = %v, want nil; output:\n%s", runErr, out.String())
			}
		})
	}
}
