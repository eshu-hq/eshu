// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"time"
)

// latencyReportSchemaVersion is the JSON schema version WriteLatencyReport
// writes. Bump it, and document the change, whenever a field's meaning
// changes or a required field is removed; scripts/compare-backend-latency.sh
// and any other reader must reject a report whose version they do not
// understand rather than silently misinterpret it.
const latencyReportSchemaVersion = 1

// LatencyReportSeedOptions is the subset of a run's seed sizing that a
// cross-backend comparison must match: two legs seeded at different scale
// measure different corpora, not different backends.
type LatencyReportSeedOptions struct {
	TotalScopes       int `json:"total_scopes"`
	NodesPerLabel     int `json:"nodes_per_label"`
	IACFactCount      int `json:"iac_fact_count"`
	SharedIntentCount int `json:"shared_intent_count"`
}

// LatencyReportIdentity identifies the run a LatencyReport came from, so a
// comparison between two reports can refuse to compare legs whose identity
// differs in a field that must match (see
// scripts/compare-backend-latency.sh). Backend and the seed/run sizing are
// always known; EshuCommit and APIBinarySHA256 are best-effort and empty
// when the caller did not supply them (-eshu-commit / -api-binary-sha256).
type LatencyReportIdentity struct {
	Backend         string                   `json:"backend"`
	EshuCommit      string                   `json:"eshu_commit,omitempty"`
	APIBinarySHA256 string                   `json:"api_binary_sha256,omitempty"`
	SeedOptions     LatencyReportSeedOptions `json:"seed_options"`
	Runs            int                      `json:"runs"`
	Iterations      int                      `json:"iterations"`
	Warmups         int                      `json:"warmups"`
}

// LatencyReportWarmStats summarizes a route's pooled warm samples (every
// counted sample from runs 2..Runs; see RouteLatency.WarmSamples). Absent
// (the route's LatencyReportRoute.Warm is nil) when Runs <= 1, since there is
// no warm pass to summarize.
type LatencyReportWarmStats struct {
	N           int     `json:"n"`
	P50MS       float64 `json:"p50_ms"`
	P95MS       float64 `json:"p95_ms"`
	MinMS       float64 `json:"min_ms"`
	MaxMS       float64 `json:"max_ms"`
	StddevMS    float64 `json:"stddev_ms"`
	RunP95MinMS float64 `json:"run_p95_min_ms"`
	RunP95MaxMS float64 `json:"run_p95_max_ms"`
}

// LatencyReportRoute is one route's full measured outcome, carrying enough
// to re-derive everything printReport shows plus the raw distribution.
type LatencyReportRoute struct {
	Route          string         `json:"route"`
	Exercised      bool           `json:"exercised"`
	Status         int            `json:"status"`
	HardFailed     bool           `json:"hard_failed"`
	HardFailedBody string         `json:"hard_failed_body,omitempty"`
	Metered        bool           `json:"metered"`
	Work           workReportWork `json:"work"`
	// ColdSamplesMS is RouteLatency.Samples (the run-1 cold pass) in
	// milliseconds, request order. Empty when the route was not exercised.
	ColdSamplesMS []float64 `json:"cold_samples_ms,omitempty"`
	// WarmSamplesMS is RouteLatency.WarmSamples pooled across runs 2..Runs,
	// in milliseconds, run then request order. Empty when Runs <= 1.
	WarmSamplesMS []float64               `json:"warm_samples_ms,omitempty"`
	Warm          *LatencyReportWarmStats `json:"warm,omitempty"`
}

// LatencyReport is the JSON document -latency-report writes: schema version,
// the identity block a comparison refuses to run across when it disagrees,
// and one LatencyReportRoute per swept route in sweep order.
type LatencyReport struct {
	SchemaVersion int                   `json:"schema_version"`
	Identity      LatencyReportIdentity `json:"identity"`
	Routes        []LatencyReportRoute  `json:"routes"`
}

// millis converts a time.Duration to fractional milliseconds for JSON
// output, matching workReportRoute.P95MS's existing convention in
// work_report.go.
func millis(d time.Duration) float64 { return float64(d) / 1e6 }

// stddevMS returns the population standard deviation of samples, in
// milliseconds. Population, not sample (n, not n-1): this describes the
// observed run, not an estimate of a larger population the run was drawn
// from.
func stddevMS(samples []time.Duration) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, d := range samples {
		sum += millis(d)
	}
	mean := sum / float64(len(samples))
	var variance float64
	for _, d := range samples {
		diff := millis(d) - mean
		variance += diff * diff
	}
	variance /= float64(len(samples))
	return math.Sqrt(variance)
}

// minMaxMS returns the min and max of samples, in milliseconds. samples must
// be non-empty.
func minMaxMS(samples []time.Duration) (min, max float64) {
	min, max = millis(samples[0]), millis(samples[0])
	for _, d := range samples[1:] {
		ms := millis(d)
		if ms < min {
			min = ms
		}
		if ms > max {
			max = ms
		}
	}
	return min, max
}

// buildLatencyReportRoute converts one RouteLatency into its report form.
func buildLatencyReportRoute(r RouteLatency) LatencyReportRoute {
	out := LatencyReportRoute{
		Route:          r.Route,
		Exercised:      r.Exercised,
		Status:         r.Status,
		HardFailed:     r.HardFailed,
		HardFailedBody: r.HardFailedBody,
		Metered:        r.Metered,
	}
	if r.Metered {
		out.Work = workReportWork{Calls: r.Work.Calls, Rows: r.Work.Rows, Blks: r.Work.Blks}
	}
	if !r.Exercised {
		return out
	}
	out.ColdSamplesMS = make([]float64, len(r.Samples))
	for i, d := range r.Samples {
		out.ColdSamplesMS[i] = millis(d)
	}
	// WarmRunP95s is required alongside WarmSamples: minMaxMS below indexes
	// samples[0] unconditionally, and sweepRoute always grows both slices
	// together, but RouteLatency is exported and nothing enforces that
	// invariant on a caller-constructed value -- degrade to no warm stats
	// rather than panic on one that violates it.
	if len(r.WarmSamples) == 0 || len(r.WarmRunP95s) == 0 {
		return out
	}
	out.WarmSamplesMS = make([]float64, len(r.WarmSamples))
	for i, d := range r.WarmSamples {
		out.WarmSamplesMS[i] = millis(d)
	}
	warmMin, warmMax := minMaxMS(r.WarmSamples)
	runP95Min, runP95Max := minMaxMS(r.WarmRunP95s)
	out.Warm = &LatencyReportWarmStats{
		N:           len(r.WarmSamples),
		P50MS:       millis(percentile(append([]time.Duration(nil), r.WarmSamples...), 0.50)),
		P95MS:       millis(percentile(append([]time.Duration(nil), r.WarmSamples...), 0.95)),
		MinMS:       warmMin,
		MaxMS:       warmMax,
		StddevMS:    stddevMS(r.WarmSamples),
		RunP95MinMS: runP95Min,
		RunP95MaxMS: runP95Max,
	}
	return out
}

// BuildLatencyReport assembles a LatencyReport from identity and the sweep's
// results, sorted by route for a deterministic diff between two reports of
// the same corpus.
func BuildLatencyReport(identity LatencyReportIdentity, results []RouteLatency) LatencyReport {
	routes := make([]LatencyReportRoute, 0, len(results))
	for _, r := range results {
		routes = append(routes, buildLatencyReportRoute(r))
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Route < routes[j].Route })
	return LatencyReport{
		SchemaVersion: latencyReportSchemaVersion,
		Identity:      identity,
		Routes:        routes,
	}
}

// WriteLatencyReport writes report as indented JSON. Called before budget
// evaluation in main.go's run(), so a leg that goes on to breach its budget
// still yields a report — the report is measurement, not a verdict.
func WriteLatencyReport(w io.Writer, report LatencyReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode latency report: %w", err)
	}
	return nil
}

// latencyReportIdentityFrom builds the identity block from the flags/env a
// run was invoked with. Backend comes from ESHU_GRAPH_BACKEND (the run
// script always sets it, defaulting to "nornicdb" when unset here too, so a
// report is never silently unidentified).
func latencyReportIdentityFrom(opts runOptions) LatencyReportIdentity {
	backend := envOr("ESHU_GRAPH_BACKEND", "nornicdb")
	runs := opts.runs
	if runs < 1 {
		runs = 1
	}
	return LatencyReportIdentity{
		Backend:         backend,
		EshuCommit:      opts.eshuCommit,
		APIBinarySHA256: opts.apiBinarySHA256,
		SeedOptions: LatencyReportSeedOptions{
			TotalScopes:       opts.totalScopes,
			NodesPerLabel:     opts.nodesPerLabel,
			IACFactCount:      opts.iacFactCount,
			SharedIntentCount: opts.sharedIntents,
		},
		Runs:       runs,
		Iterations: opts.iterations,
		Warmups:    warmupRequests,
	}
}

// writeLatencyReportFile writes the full latency report (identity block plus
// every route's cold/warm samples and warm distribution stats) to path, or
// does nothing when path is empty.
func writeLatencyReportFile(path string, opts runOptions, results []RouteLatency) error {
	if path == "" {
		return nil
	}
	report := BuildLatencyReport(latencyReportIdentityFrom(opts), results)
	f, err := os.Create(path) // #nosec G304 -- path is the -latency-report CLI flag, operator-controlled
	if err != nil {
		return fmt.Errorf("create latency report %s: %w", path, err)
	}
	if err := WriteLatencyReport(f, report); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close latency report %s: %w", path, err)
	}
	return nil
}
