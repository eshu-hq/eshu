// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// findingFor returns the finding with the given check name, or nil.
func findingFor(r Report, check string) *Finding {
	for i := range r.Findings {
		if r.Findings[i].Check == check {
			return &r.Findings[i]
		}
	}
	return nil
}

func baselineFixture() PhaseBaseline {
	return PhaseBaseline{
		SchemaVersion:  "1",
		BaselineID:     "test-corpus-nornicdb",
		RegressionBand: 0.15,
		Phases: map[string]PhaseBaselineEntry{
			"bootstrap":   {BaselineSeconds: 10, Gated: true},
			"drains":      {BaselineSeconds: 40, Gated: true},
			"settle":      {BaselineSeconds: 20, Gated: false, Note: "fixed sleep"},
			"graph_query": {BaselineSeconds: 30, Gated: true},
		},
	}
}

func TestEvaluatePhaseTimings(t *testing.T) {
	t.Run("all phases within band passes", func(t *testing.T) {
		var r Report
		// 10*1.15=11.5, 40*1.15=46, 30*1.15=34.5 — all under.
		obs := PhaseTimings{Phases: map[string]float64{
			"bootstrap": 11, "drains": 45, "settle": 25, "graph_query": 33,
		}}
		evaluatePhaseTimings(obs, baselineFixture(), 0, false, &r)
		if r.Failed() {
			t.Fatalf("within-band run must pass; findings: %+v", r.Findings)
		}
	})

	t.Run("gated phase over band fails", func(t *testing.T) {
		var r Report
		obs := PhaseTimings{Phases: map[string]float64{
			"bootstrap": 11, "drains": 47, "settle": 25, "graph_query": 33, // drains 47 > 46 ceiling
		}}
		evaluatePhaseTimings(obs, baselineFixture(), 0, false, &r)
		if !r.Failed() {
			t.Fatalf("gated drains regression beyond band must fail; findings: %+v", r.Findings)
		}
		f := findingFor(r, "phase_drains")
		if f == nil || f.OK || !f.Required {
			t.Fatalf("phase_drains must be a failing required finding; got %+v", f)
		}
	})

	t.Run("non-gated phase over band does not fail", func(t *testing.T) {
		var r Report
		// settle blown way past its band, everything else fine.
		obs := PhaseTimings{Phases: map[string]float64{
			"bootstrap": 10, "drains": 40, "settle": 100, "graph_query": 30,
		}}
		evaluatePhaseTimings(obs, baselineFixture(), 0, false, &r)
		if r.Failed() {
			t.Fatalf("non-gated settle regression must not fail the gate; findings: %+v", r.Findings)
		}
		f := findingFor(r, "phase_settle")
		if f == nil || f.Required {
			t.Fatalf("phase_settle must be advisory (Required=false); got %+v", f)
		}
	})

	t.Run("missing observed gated phase fails", func(t *testing.T) {
		var r Report
		obs := PhaseTimings{Phases: map[string]float64{
			"bootstrap": 10, "settle": 20, "graph_query": 30, // drains missing
		}}
		evaluatePhaseTimings(obs, baselineFixture(), 0, false, &r)
		if !r.Failed() {
			t.Fatalf("a missing gated phase must fail (cannot prove no regression); findings: %+v", r.Findings)
		}
		f := findingFor(r, "phase_drains")
		if f == nil || f.OK || !f.Required {
			t.Fatalf("missing phase_drains must be a failing required finding; got %+v", f)
		}
	})

	t.Run("band override loosens the gate", func(t *testing.T) {
		var r Report
		// drains 60 vs baseline 40: 50% over. Default 15% band fails; 100% passes.
		obs := PhaseTimings{Phases: map[string]float64{
			"bootstrap": 10, "drains": 60, "settle": 20, "graph_query": 30,
		}}
		evaluatePhaseTimings(obs, baselineFixture(), 1.0, false, &r)
		if r.Failed() {
			t.Fatalf("100%% band override must pass a 50%% drains regression; findings: %+v", r.Findings)
		}
	})

	t.Run("advisory mode downgrades a gated regression to non-blocking", func(t *testing.T) {
		var r Report
		// drains blown past band: would fail in blocking mode, must only WARN here.
		obs := PhaseTimings{Phases: map[string]float64{
			"bootstrap": 10, "drains": 80, "settle": 20, "graph_query": 30,
		}}
		evaluatePhaseTimings(obs, baselineFixture(), 0, true, &r)
		if r.Failed() {
			t.Fatalf("advisory mode must not fail the gate; findings: %+v", r.Findings)
		}
		f := findingFor(r, "phase_drains")
		if f == nil || f.OK || f.Required {
			t.Fatalf("advisory drains regression must be a non-OK, non-required finding; got %+v", f)
		}
	})

	t.Run("absolute slack absorbs small-phase integer jitter", func(t *testing.T) {
		// A 3s phase ticking to 4s is +33%, beyond the 15% band, but within a 2s
		// absolute slack — it must pass.
		base := PhaseBaseline{
			SchemaVersion: "1", RegressionBand: 0.15, AbsoluteSlackSeconds: 2,
			Phases: map[string]PhaseBaselineEntry{"graph_query": {BaselineSeconds: 3, Gated: true}},
		}
		var r Report
		evaluatePhaseTimings(PhaseTimings{Phases: map[string]float64{"graph_query": 4}}, base, 0, false, &r)
		if r.Failed() {
			t.Fatalf("4s vs 3s baseline within +2s slack must pass; findings: %+v", r.Findings)
		}
		// But a jump past both band and slack still fails.
		var r2 Report
		evaluatePhaseTimings(PhaseTimings{Phases: map[string]float64{"graph_query": 6}}, base, 0, false, &r2)
		if !r2.Failed() {
			t.Fatalf("6s vs 3s baseline (beyond +2s slack and 15%%) must fail; findings: %+v", r2.Findings)
		}
	})

	t.Run("observed phase without baseline is advisory", func(t *testing.T) {
		var r Report
		obs := PhaseTimings{Phases: map[string]float64{
			"bootstrap": 10, "drains": 40, "settle": 20, "graph_query": 30,
			"newphase": 5,
		}}
		evaluatePhaseTimings(obs, baselineFixture(), 0, false, &r)
		if r.Failed() {
			t.Fatalf("an unbaselined observed phase must not fail the gate; findings: %+v", r.Findings)
		}
		f := findingFor(r, "phase_newphase_unbaselined")
		if f == nil || f.OK || f.Required {
			t.Fatalf("unbaselined phase must be an advisory non-OK finding; got %+v", f)
		}
	})
}

func TestLoadPhaseTimings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "phase-timings.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"1","phases":{"bootstrap":12.5,"drains":40.2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	pt, err := LoadPhaseTimings(path)
	if err != nil {
		t.Fatalf("LoadPhaseTimings: %v", err)
	}
	if pt.Phases["bootstrap"] != 12.5 || pt.Phases["drains"] != 40.2 {
		t.Fatalf("unexpected phases: %+v", pt.Phases)
	}

	t.Run("empty phases rejected", func(t *testing.T) {
		p := filepath.Join(dir, "empty.json")
		if err := os.WriteFile(p, []byte(`{"schema_version":"1","phases":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPhaseTimings(p); err == nil {
			t.Fatal("empty phases must be rejected")
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		if _, err := LoadPhaseTimings(filepath.Join(dir, "nope.json")); err == nil {
			t.Fatal("missing file must error")
		}
	})
}

func TestLoadPhaseBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e-baseline.json")
	body := `{"schema_version":"1","baseline_id":"x","regression_band":0.15,"phases":{"bootstrap":{"baseline_seconds":10,"gated":true}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	pb, err := LoadPhaseBaseline(path)
	if err != nil {
		t.Fatalf("LoadPhaseBaseline: %v", err)
	}
	if pb.RegressionBand != 0.15 || !pb.Phases["bootstrap"].Gated {
		t.Fatalf("unexpected baseline: %+v", pb)
	}

	t.Run("non-positive band rejected", func(t *testing.T) {
		p := filepath.Join(dir, "badband.json")
		if err := os.WriteFile(p, []byte(`{"schema_version":"1","regression_band":0,"phases":{"a":{"baseline_seconds":1,"gated":true}}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPhaseBaseline(p); err == nil {
			t.Fatal("non-positive regression_band must be rejected")
		}
	})

	t.Run("empty phases rejected", func(t *testing.T) {
		p := filepath.Join(dir, "nophases.json")
		if err := os.WriteFile(p, []byte(`{"schema_version":"1","regression_band":0.15,"phases":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPhaseBaseline(p); err == nil {
			t.Fatal("empty phases must be rejected")
		}
	})
}
