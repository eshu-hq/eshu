// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBuildLatencyReportIncludesColdAndWarmStats proves BuildLatencyReport
// carries both the cold pass and the warm distribution (n/p50/p95/min/max/
// stddev, plus the per-run p95 spread) through to the JSON-serializable form,
// for a route swept over multiple runs.
func TestBuildLatencyReportIncludesColdAndWarmStats(t *testing.T) {
	results := []RouteLatency{
		{
			Route:     "GET /warm",
			Exercised: true,
			Status:    200,
			Samples:   []time.Duration{10 * time.Millisecond, 12 * time.Millisecond},
			WarmSamples: []time.Duration{
				20 * time.Millisecond, 22 * time.Millisecond, 24 * time.Millisecond, 26 * time.Millisecond,
			},
			WarmRunP95s: []time.Duration{22 * time.Millisecond, 26 * time.Millisecond},
		},
	}
	report := BuildLatencyReport(LatencyReportIdentity{Backend: "nornicdb", Runs: 3, Iterations: 2, Warmups: 2}, results)

	if report.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", report.SchemaVersion)
	}
	if len(report.Routes) != 1 {
		t.Fatalf("len(Routes) = %d, want 1", len(report.Routes))
	}
	r := report.Routes[0]
	if len(r.ColdSamplesMS) != 2 {
		t.Errorf("len(ColdSamplesMS) = %d, want 2", len(r.ColdSamplesMS))
	}
	if len(r.WarmSamplesMS) != 4 {
		t.Errorf("len(WarmSamplesMS) = %d, want 4", len(r.WarmSamplesMS))
	}
	if r.Warm == nil {
		t.Fatalf("Warm = nil, want stats populated when WarmSamples is non-empty")
	}
	if r.Warm.N != 4 {
		t.Errorf("Warm.N = %d, want 4", r.Warm.N)
	}
	if r.Warm.MinMS != 20 || r.Warm.MaxMS != 26 {
		t.Errorf("Warm.Min/Max = %v/%v, want 20/26", r.Warm.MinMS, r.Warm.MaxMS)
	}
	if r.Warm.RunP95MinMS != 22 || r.Warm.RunP95MaxMS != 26 {
		t.Errorf("Warm.RunP95Min/Max = %v/%v, want 22/26", r.Warm.RunP95MinMS, r.Warm.RunP95MaxMS)
	}
	if r.Warm.StddevMS <= 0 {
		t.Errorf("Warm.StddevMS = %v, want > 0 for a non-constant sample set", r.Warm.StddevMS)
	}

	// Round-trips through JSON: the schema is what scripts/compare-backend-latency.sh reads.
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"run_p95_max_ms"`) {
		t.Errorf("marshaled report is missing the run_p95_max_ms field:\n%s", data)
	}
	var roundTripped LatencyReport
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if roundTripped.Routes[0].Warm.P95MS != r.Warm.P95MS {
		t.Errorf("round-tripped Warm.P95MS = %v, want %v", roundTripped.Routes[0].Warm.P95MS, r.Warm.P95MS)
	}
}

// TestBuildLatencyReportDoesNotPanicWhenWarmRunP95sIsShorterThanWarmSamples
// guards a real panic: minMaxMS indexes samples[0] unconditionally, and
// buildLatencyReportRoute called it on WarmRunP95s right after checking only
// len(WarmSamples) == 0. A RouteLatency with WarmSamples populated but
// WarmRunP95s empty (an invariant sweepRoute always keeps in sync today, but
// this type is exported and nothing in the compiler enforces it) panicked
// with "index out of range [0] with length 0" instead of degrading
// gracefully.
func TestBuildLatencyReportDoesNotPanicWhenWarmRunP95sIsShorterThanWarmSamples(t *testing.T) {
	results := []RouteLatency{
		{
			Route:       "GET /malformed",
			Exercised:   true,
			Status:      200,
			Samples:     []time.Duration{10 * time.Millisecond},
			WarmSamples: []time.Duration{20 * time.Millisecond, 22 * time.Millisecond},
			WarmRunP95s: nil,
		},
	}
	report := BuildLatencyReport(LatencyReportIdentity{Backend: "nornicdb", Runs: 3, Iterations: 2, Warmups: 2}, results)
	if got := report.Routes[0].Warm; got != nil {
		t.Errorf("Warm = %+v, want nil when WarmRunP95s is empty despite non-empty WarmSamples", got)
	}
}

// TestBuildLatencyReportOmitsWarmWhenRunsIsOne proves the default (Runs=1,
// no warm passes) leaves Warm nil rather than a zero-valued stats block that
// would misread as "warm samples exist and are all zero."
func TestBuildLatencyReportOmitsWarmWhenRunsIsOne(t *testing.T) {
	results := []RouteLatency{
		{Route: "GET /cold-only", Exercised: true, Status: 200, Samples: []time.Duration{5 * time.Millisecond}},
	}
	report := BuildLatencyReport(LatencyReportIdentity{Backend: "nornicdb", Runs: 1, Iterations: 1, Warmups: 2}, results)
	if report.Routes[0].Warm != nil {
		t.Errorf("Warm = %+v, want nil when Runs<=1 (no warm pass ever ran)", report.Routes[0].Warm)
	}
	if len(report.Routes[0].WarmSamplesMS) != 0 {
		t.Errorf("WarmSamplesMS = %v, want empty", report.Routes[0].WarmSamplesMS)
	}
}

// TestBuildLatencyReportSkipsSamplesForNotExercisedRoute proves a
// not-exercised route (a 4xx on the warmup probe) carries no fabricated
// sample data — Samples/WarmSamples are both nil on the source RouteLatency,
// and the report must not synthesize stats from nothing.
func TestBuildLatencyReportSkipsSamplesForNotExercisedRoute(t *testing.T) {
	results := []RouteLatency{{Route: "GET /needs-scope", Exercised: false, Status: 400}}
	report := BuildLatencyReport(LatencyReportIdentity{Backend: "nornicdb", Runs: 1, Iterations: 20, Warmups: 2}, results)
	r := report.Routes[0]
	if r.ColdSamplesMS != nil || r.WarmSamplesMS != nil || r.Warm != nil {
		t.Errorf("not-exercised route carries sample data: %+v", r)
	}
}

// TestLatencyReportWrittenBeforeBudgetEvaluationEvenOnBreach mirrors the
// exact composition main.go's run() uses (sweep, THEN write the latency
// report, THEN evaluate budgets): a route whose sweep result breaches its
// budget must still have a report on disk, because the report is
// measurement, not a verdict, and a breaching leg is exactly the leg an
// operator most needs the raw samples from.
func TestLatencyReportWrittenBeforeBudgetEvaluationEvenOnBreach(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond) // seeded violation: over the 10ms budget below
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL: srv.URL, APIKey: "test-key", Routes: []string{"GET /seeded-slow-route"},
		Iterations: 2, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}

	path := filepath.Join(t.TempDir(), "report.json")
	opts := runOptions{iterations: 2, runs: 1}
	if err := writeLatencyReportFile(path, opts, results); err != nil {
		t.Fatalf("writeLatencyReportFile: %v", err)
	}

	budgets, err := ParseRouteBudgets(strings.NewReader("default\t10\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	breaches := EvaluateBudgets(results, budgets)
	if len(breaches) != 1 {
		t.Fatalf("breaches = %d, want 1 (a 50ms handler over a 10ms budget)", len(breaches))
	}

	data, err := os.ReadFile(path) // #nosec G304 -- test-owned temp path
	if err != nil {
		t.Fatalf("report was not written despite the budget breach: %v", err)
	}
	var report LatencyReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	if len(report.Routes) != 1 || report.Routes[0].Route != "GET /seeded-slow-route" {
		t.Errorf("report.Routes = %+v, want the one breaching route", report.Routes)
	}
}

// TestWriteLatencyReportFileNoopOnEmptyPath matches writeWorkReportFile's own
// convention: an empty path is "don't write a report", not an error.
func TestWriteLatencyReportFileNoopOnEmptyPath(t *testing.T) {
	if err := writeLatencyReportFile("", runOptions{}, nil); err != nil {
		t.Fatalf("writeLatencyReportFile(\"\", ...) = %v, want nil", err)
	}
}
