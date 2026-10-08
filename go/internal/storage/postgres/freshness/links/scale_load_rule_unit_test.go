// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCheckScaleLoadsQuietRoundPasses is the PD quiet round: every sample,
// start through end, below half the CPU count.
func TestCheckScaleLoadsQuietRoundPasses(t *testing.T) {
	t.Parallel()
	v := checkScaleLoads(8, []float64{5.06, 5.08, 4.95, 7.59, 5.10})
	if !v.Valid {
		t.Fatalf("quiet round invalid: %q", v.Reason)
	}
	if v.Reason != "" {
		t.Fatalf("quiet round carries a reason: %q", v.Reason)
	}
}

// TestCheckScaleLoadsPlantedSpikeFails is the blind spot the old rule had: a
// mid-run burst the start sample never sees. The old G7/G8 check (start
// sample below the full CPU count) accepted this round; PD rejects it.
func TestCheckScaleLoadsPlantedSpikeFails(t *testing.T) {
	t.Parallel()
	v := checkScaleLoads(8, []float64{5.06, 5.08, 26.0, 7.96, 5.10})
	if v.Valid {
		t.Fatal("planted loaded round accepted, want invalid")
	}
	if v.Reason == "" {
		t.Fatal("invalid round carries no reason")
	}
}

// TestCheckScaleLoadsUnreadableFails: a -1 sample means the load could not be
// read; the round proves nothing and is invalid.
func TestCheckScaleLoadsUnreadableFails(t *testing.T) {
	t.Parallel()
	for _, loads := range [][]float64{{-1}, {5.0, -1, 5.1}} {
		if v := checkScaleLoads(8, loads); v.Valid {
			t.Fatalf("unreadable loads %v accepted, want invalid", loads)
		}
	}
}

// TestCheckScaleLoadsEmptyFails: a round with no samples proves nothing.
func TestCheckScaleLoadsEmptyFails(t *testing.T) {
	t.Parallel()
	if v := checkScaleLoads(8, nil); v.Valid {
		t.Fatal("empty sample set accepted, want invalid")
	}
}

// TestCheckScaleLoadsAtThresholdFails: PD requires strictly below half the
// CPU count; exactly at the threshold is invalid.
func TestCheckScaleLoadsAtThresholdFails(t *testing.T) {
	t.Parallel()
	if v := checkScaleLoads(8, []float64{5.0, 8.0, 5.1}); v.Valid {
		t.Fatal("at-threshold sample accepted, want invalid")
	}
}

// TestCheckScaleLoadsBadThresholdFails: without a positive threshold there is
// no rule to check against.
func TestCheckScaleLoadsBadThresholdFails(t *testing.T) {
	t.Parallel()
	for _, threshold := range []float64{0, -8} {
		if v := checkScaleLoads(threshold, []float64{1.0}); v.Valid {
			t.Fatalf("threshold %v accepted, want invalid", threshold)
		}
	}
}

// TestCheckScaleCanaryWithinBoundPasses: the base-binary control ran inside
// its bound, so the backend was not starved.
func TestCheckScaleCanaryWithinBoundPasses(t *testing.T) {
	t.Parallel()
	v := checkScaleCanary(5.86, 12)
	if !v.Valid {
		t.Fatalf("in-bound canary invalid: %q", v.Reason)
	}
}

// TestCheckScaleCanaryOverBoundFails: an over-bound control invalidates the
// round on both sides, per PD4.
func TestCheckScaleCanaryOverBoundFails(t *testing.T) {
	t.Parallel()
	v := checkScaleCanary(39.46, 12)
	if v.Valid {
		t.Fatal("over-bound canary accepted, want invalid")
	}
	if v.Reason == "" {
		t.Fatal("invalid canary carries no reason")
	}
}

// TestCheckScaleCanaryFailedRunFails: a negative canary time means the
// control did not run; the round is invalid, not quietly valid.
func TestCheckScaleCanaryFailedRunFails(t *testing.T) {
	t.Parallel()
	if v := checkScaleCanary(-1, 12); v.Valid {
		t.Fatal("failed canary run accepted, want invalid")
	}
}

// TestStartScaleLoadSampler records the start sample first, the end sample
// last, and every tick between them.
func TestStartScaleLoadSampler(t *testing.T) {
	t.Parallel()
	var reads int
	stop := startScaleLoadSampler(func() float64 {
		reads++
		return float64(reads)
	}, time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	samples := stop()
	if len(samples) < 3 {
		t.Fatalf("samples = %d, want at least start + tick + end", len(samples))
	}
	if samples[0] != 1 {
		t.Fatalf("first sample = %v, want the synchronous start read 1", samples[0])
	}
	for i := 1; i < len(samples); i++ {
		if samples[i] != samples[i-1]+1 {
			t.Fatalf("samples not in read order: %v", samples)
		}
	}
}

// TestStartScaleLoadSamplerNoTick samples start and end only when the tick is
// not positive.
func TestStartScaleLoadSamplerNoTick(t *testing.T) {
	t.Parallel()
	var reads int
	stop := startScaleLoadSampler(func() float64 {
		reads++
		return float64(reads)
	}, 0)
	samples := stop()
	if len(samples) != 2 || samples[0] != 1 || samples[1] != 2 {
		t.Fatalf("samples = %v, want [1 2]", samples)
	}
}

// TestWaitForScaleQuietImmediate returns the quiet load without waiting.
func TestWaitForScaleQuietImmediate(t *testing.T) {
	t.Parallel()
	var waits int
	load := waitForScaleQuiet(func() float64 { return 5.0 }, 8, time.Minute, time.Now().Add(time.Hour), func(float64) { waits++ })
	if load != 5.0 {
		t.Fatalf("load = %v, want 5.0", load)
	}
	if waits != 0 {
		t.Fatalf("waits = %d, want 0", waits)
	}
}

// TestWaitForScaleQuietBecomesQuiet waits out a loaded host and returns the
// first quiet sample.
func TestWaitForScaleQuietBecomesQuiet(t *testing.T) {
	t.Parallel()
	reads := []float64{12.0, 30.0, 7.9}
	var waits int
	var got []float64
	load := waitForScaleQuiet(func() float64 {
		v := reads[0]
		if len(reads) > 1 {
			reads = reads[1:]
		}
		return v
	}, 8, time.Millisecond, time.Now().Add(time.Minute), func(load float64) {
		waits++
		got = append(got, load)
	})
	if load != 7.9 {
		t.Fatalf("load = %v, want 7.9", load)
	}
	if waits != 2 || len(got) != 2 || got[0] != 12.0 || got[1] != 30.0 {
		t.Fatalf("waits = %d %v, want 2 [12 30]", waits, got)
	}
}

// TestWaitForScaleQuietTimeout returns the last load without sleeping once
// the stop time has passed.
func TestWaitForScaleQuietTimeout(t *testing.T) {
	t.Parallel()
	var waits int
	began := time.Now()
	load := waitForScaleQuiet(func() float64 { return 30.0 }, 8, time.Hour, time.Now().Add(-time.Second), func(float64) { waits++ })
	if load != 30.0 {
		t.Fatalf("load = %v, want 30.0", load)
	}
	if waits != 0 {
		t.Fatalf("waits = %d, want 0", waits)
	}
	if elapsed := time.Since(began); elapsed > 30*time.Second {
		t.Fatalf("wait slept %v past the stop time", elapsed)
	}
}

// TestResolveScaleBaseRef prefers the merge base with origin/main.
func TestResolveScaleBaseRef(t *testing.T) {
	t.Parallel()
	ref, err := resolveScaleBaseRef(func(name string, args ...string) (string, error) {
		return "abc123\n", nil
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ref != "abc123" {
		t.Fatalf("ref = %q, want the trimmed merge base", ref)
	}
}

// TestResolveScaleBaseRefFallsBackToHEAD uses HEAD when the merge base is
// unavailable (a checkout without origin/main).
func TestResolveScaleBaseRefFallsBackToHEAD(t *testing.T) {
	t.Parallel()
	ref, err := resolveScaleBaseRef(func(name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "merge-base" {
			return "", errors.New("no origin/main")
		}
		return "def456\n", nil
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ref != "def456" {
		t.Fatalf("ref = %q, want HEAD", ref)
	}
}

// TestResolveScaleBaseRefFailsWithoutGit reports the failure instead of
// guessing a base.
func TestResolveScaleBaseRefFailsWithoutGit(t *testing.T) {
	t.Parallel()
	if _, err := resolveScaleBaseRef(func(name string, args ...string) (string, error) {
		return "", errors.New("no git")
	}); err == nil {
		t.Fatal("resolve without git succeeded, want an error")
	}
}

// TestScaleLoadThresholdDefaultsToHalfCPUs: the PD headroom bound, read from
// the machine the harness runs on.
func TestScaleLoadThresholdDefaultsToHalfCPUs(t *testing.T) {
	t.Setenv(scaleLoadThresholdEnv, "")
	if got, want := scaleLoadThreshold(), float64(runtime.NumCPU())/2; got != want {
		t.Fatalf("threshold = %v, want %v", got, want)
	}
}

// TestScaleLoadThresholdOverride parses the env knob and falls back to the
// default on garbage.
func TestScaleLoadThresholdOverride(t *testing.T) {
	t.Setenv(scaleLoadThresholdEnv, "4.5")
	if got := scaleLoadThreshold(); got != 4.5 {
		t.Fatalf("threshold = %v, want 4.5", got)
	}
	t.Setenv(scaleLoadThresholdEnv, "half")
	if got, want := scaleLoadThreshold(), float64(runtime.NumCPU())/2; got != want {
		t.Fatalf("garbage threshold = %v, want default %v", got, want)
	}
	t.Setenv(scaleLoadThresholdEnv, "-2")
	if got, want := scaleLoadThreshold(), float64(runtime.NumCPU())/2; got != want {
		t.Fatalf("negative threshold = %v, want default %v", got, want)
	}
}

// TestScaleControlMaxSecondsDefaultsToDerivedBound: the canary bound fixed
// from the committed G7 bare_b spread (max 8.50s, mean 5.86, sd 1.24,
// n=10), sitting outside it.
func TestScaleControlMaxSecondsDefaultsToDerivedBound(t *testing.T) {
	t.Setenv(scaleControlMaxSecondsEnv, "")
	if got := scaleControlMaxSeconds(); got != scaleDefaultControlMaxSeconds {
		t.Fatalf("canary bound = %v, want default %v", got, scaleDefaultControlMaxSeconds)
	}
	t.Setenv(scaleControlMaxSecondsEnv, "20")
	if got := scaleControlMaxSeconds(); got != 20 {
		t.Fatalf("canary bound = %v, want 20", got)
	}
	t.Setenv(scaleControlMaxSecondsEnv, "soon")
	if got := scaleControlMaxSeconds(); got != scaleDefaultControlMaxSeconds {
		t.Fatalf("garbage canary bound = %v, want default %v", got, scaleDefaultControlMaxSeconds)
	}
}

// TestScaleCanaryBoundCoversCommittedG7Spread locks the canary bound
// derivation: the default must sit strictly outside the committed G7 bare_b
// spread (docs/internal/evidence/7127-link-writer-g7-results.json), per rule 2
// of the timing-proof rules (a bound must sit outside the measured spread).
func TestScaleCanaryBoundCoversCommittedG7Spread(t *testing.T) {
	raw, err := os.ReadFile("../../../../../../docs/internal/evidence/7127-link-writer-g7-results.json")
	if err != nil {
		t.Fatalf("committed G7 results: %v", err)
	}
	var doc struct {
		Events []struct {
			Event       string  `json:"event"`
			BareSeconds float64 `json:"bare_b_seconds"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse G7 results: %v", err)
	}
	var max float64
	var n int
	for _, e := range doc.Events {
		if e.Event != "g7_round" {
			continue
		}
		n++
		if e.BareSeconds > max {
			max = e.BareSeconds
		}
	}
	if n == 0 {
		t.Fatal("no g7_round events in the committed results")
	}
	if scaleDefaultControlMaxSeconds <= max {
		t.Fatalf("canary bound %.2f does not cover the committed bare_b max %.2f over %d rounds", scaleDefaultControlMaxSeconds, max, n)
	}
}

// TestScaleBaseRefEnvWins: an explicit base ref needs no git resolution.
func TestScaleBaseRefEnvWins(t *testing.T) {
	t.Setenv(scaleBaseRefEnv, "deadbeef")
	if got := scaleBaseRef(t); got != "deadbeef" {
		t.Fatalf("base ref = %q, want the env value", got)
	}
}

// TestDeclareScaleHostWritesGateFile: the PD1 record carries the label and
// the host's uptime line.
func TestDeclareScaleHostWritesGateFile(t *testing.T) {
	path := t.TempDir() + "/gate.log"
	t.Setenv(scaleGateFileEnv, path)
	declareScaleHost(t, "test-label")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("gate file: %v", err)
	}
	if !strings.Contains(string(raw), "== test-label ") {
		t.Fatalf("gate file lacks the label line:\n%s", raw)
	}
	if !strings.Contains(string(raw), "load average") {
		t.Fatalf("gate file lacks the uptime line:\n%s", raw)
	}
}

// TestDeclareScaleHostWithoutGateFileIsNoOp: unset gate file, nothing to
// declare and no failure.
func TestDeclareScaleHostWithoutGateFileIsNoOp(t *testing.T) {
	t.Setenv(scaleGateFileEnv, "")
	declareScaleHost(t, "test-label")
}
