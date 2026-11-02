// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow/parity"
)

// Criterion statuses. NOT_APPLICABLE carries the §3 sentinel code; it is
// never a PASS.
const (
	StatusPass          = "PASS"
	StatusFail          = "FAIL"
	StatusNotApplicable = "NOT_APPLICABLE"
	StatusNotAvailable  = "NOT_AVAILABLE"
	// StatusVacuous marks a check that passed without being able to fail,
	// for example the §4 dry claim on an instance that holds no rows.
	StatusVacuous = "VACUOUS"
)

// Verdicts for a whole seed and leg.
const (
	VerdictClean      = "CLEAN"
	VerdictFailed     = "FAILED"
	VerdictAborted    = "ABORTED"
	VerdictIncomplete = "INCOMPLETE"
	VerdictOverhead   = "OVERHEAD_MEASUREMENT_ONLY"
)

// Criterion is one §6 pass criterion on one leg.
type Criterion struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Phases are the run's timestamps and phase durations.
type Phases struct {
	Start       time.Time `json:"start"`
	WindowStart time.Time `json:"window_start"`
	DrainStart  time.Time `json:"drain_start"`
	WorkEnd     time.Time `json:"work_end"`
	End         time.Time `json:"end"`
	SeedSeconds float64   `json:"seed_seconds"`
	// StatsWaitSeconds is the wait for the post-seed ANALYZE; StatsReady
	// is false when the bound expired first (the window then ran on cold
	// planner statistics, and claim latency reflects that).
	StatsWaitSeconds float64 `json:"stats_wait_seconds"`
	StatsReady       bool    `json:"stats_ready"`
	Drained          bool    `json:"drained"`
}

// CoreResidue is the third zero: core rows carrying the prefix.
type CoreResidue struct {
	Status  string `json:"status"`
	Checked bool   `json:"checked"`
	Rows    int    `json:"rows"`
	Detail  string `json:"detail,omitempty"`
}

// Residue is the §6 three-zero result after cleanup.
type Residue struct {
	FirstZero       int         `json:"first_zero_synthetic_rows"`
	SecondZeroDelta []string    `json:"second_zero_non_synthetic_deltas"`
	ThirdZero       CoreResidue `json:"third_zero_core"`
}

// HeadroomReport is the pg_headroom source, baseline, and peak readings.
type HeadroomReport struct {
	Source   string          `json:"source"`
	Limits   Headroom        `json:"limits"`
	Baseline *HeadroomSample `json:"baseline,omitempty"`
	Peak     *HeadroomSample `json:"peak,omitempty"`
	CPU      string          `json:"cpu_memory"`
	Errors   []string        `json:"errors,omitempty"`
}

// Report is the machine-readable result of one seed on one leg.
type Report struct {
	Leg                string                    `json:"leg"`
	Target             Target                    `json:"target"`
	Fields             map[string]string         `json:"spec_fields"`
	IntreeDatabaseName string                    `json:"intree_database_name"`
	DatabaseName       string                    `json:"database_name"`
	Seed               int64                     `json:"seed"`
	Workers            int                       `json:"workers"`
	WorkerMultiple     int                       `json:"worker_multiple"`
	FullSize           bool                      `json:"spec_full_size"`
	Mix                Mix                       `json:"mix"`
	Phases             Phases                    `json:"phases"`
	Created            int64                     `json:"created_items"`
	MaxItems           int                       `json:"queue_depth_cap"`
	Terminal           int                       `json:"terminal_acked_items"`
	QuotaHeld          int                       `json:"quota_held_items"`
	OpsTotal           int                       `json:"ops_total"`
	OpsPerSecond       float64                   `json:"ops_per_second"`
	ClientLatency      map[string]LatencySummary `json:"client_latency"`
	SlowestCalls       []SlowCall                `json:"slowest_calls"`
	ServerLatency      string                    `json:"server_latency"`
	Codes              map[string]map[string]int `json:"codes"`
	Anomalies          map[string]int            `json:"anomalies"`
	AnomalySamples     []string                  `json:"anomaly_samples,omitempty"`
	Reclaim            ReclaimReport             `json:"reclaim"`
	StaleWrites        map[string]int            `json:"stale_writes"`
	Advisory           map[string]any            `json:"advisory"`
	Pins               map[string]int            `json:"pins"`
	Quota              []QuotaBucket             `json:"quota_buckets"`
	UnclaimedScopes    []string                  `json:"unclaimed_scopes_in_window"`
	NonTerminal        []string                  `json:"non_terminal_after_drain"`
	Sampler            map[string]any            `json:"sampler"`
	Samples            []SampleRecord            `json:"samples"`
	Headroom           HeadroomReport            `json:"pg_headroom"`
	Baseline           Census                    `json:"baseline_census"`
	Final              Census                    `json:"final_census"`
	Deleted            map[string]int            `json:"deleted_rows"`
	// CleanupSkipped says why the reversal deleted nothing, when it did not.
	CleanupSkipped string               `json:"cleanup_skipped,omitempty"`
	Residue        Residue              `json:"residue"`
	NotApplicable  map[string]string    `json:"not_applicable"`
	SpecReadings   []SpecReading        `json:"spec_readings"`
	IsolationProbe Criterion            `json:"isolation_probe"`
	Criteria       map[string]Criterion `json:"criteria"`
	Abort          *Violation           `json:"abort,omitempty"`
	Verdict        string               `json:"verdict"`
	// Incomplete names why a failure-free run is INCOMPLETE, when it is.
	Incomplete string `json:"incomplete_reason,omitempty"`
}

// ReclaimReport is the §6 reclaim measurement over the abandoned claims:
// reap time minus the later of lease expiry and commit pin.
type ReclaimReport struct {
	Abandoned int            `json:"abandoned"`
	Reaped    int            `json:"reaped"`
	Latency   LatencySummary `json:"reap_after_expiry"`
	BoundMS   float64        `json:"bound_ms"`
	Late      []string       `json:"late,omitempty"`
	PinEarly  []string       `json:"reaped_before_pin_deadline,omitempty"`
	NotReaped []string       `json:"not_reaped,omitempty"`
}

func newReport(spec Spec, leg Leg, opt Options) *Report {
	r := &Report{
		Leg: leg.Name, Target: spec.Target, Fields: spec.Fields, Seed: spec.Seed,
		Workers: spec.Workers(), WorkerMultiple: spec.WorkerMultiple, FullSize: spec.FullSize(),
		Mix: opt.Mix, DatabaseName: leg.DatabaseName, ServerLatency: leg.ServerLatency,
		NotApplicable: map[string]string{}, Criteria: map[string]Criterion{},
		IntreeDatabaseName: NotApplicable, SpecReadings: SpecReadings(),
	}
	if leg.Name == "in-tree" {
		r.IntreeDatabaseName = leg.DatabaseName
	}
	r.Headroom.Limits = spec.Headroom
	r.Headroom.CPU = "NOT_AVAILABLE from this source; the live leg enforces CPU and memory through the abort file"
	if leg.Headroom != nil {
		r.Headroom.Source = leg.Headroom.Name()
	} else {
		r.Headroom.Source = StatusNotAvailable
	}
	if !leg.Advisory {
		r.NotApplicable["step6_advisory"] = parity.ErrGuardedAdmissionNotSupported.Error()
	}
	if !leg.Pin {
		r.NotApplicable["step7_pin"] = parity.ErrPinNotSupported.Error()
	}
	if !leg.Quota || spec.QuotaBudget == 0 {
		r.NotApplicable["step5_quota"] = "no quota budget enforced on this leg (claim plane without --quotas, or quota_budget NOT_APPLICABLE)"
	}
	return r
}

// JSON renders the report for the evidence record.
func (r *Report) JSON() ([]byte, error) {
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal contention report: %w", err)
	}
	return body, nil
}

// Summary renders the human summary: verdict, criteria, and headline numbers.
func (r *Report) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "contention %s leg (%s), seed %d, W=%d (%dx), verdict %s\n",
		r.Leg, r.Target, r.Seed, r.Workers, r.WorkerMultiple, r.Verdict)
	if r.Abort != nil {
		fmt.Fprintf(&b, "  ABORT %s: %s\n", r.Abort.Trigger, r.Abort.Detail)
	}
	dur := r.Phases.WorkEnd.Sub(r.Phases.WindowStart)
	fmt.Fprintf(&b, "  created %d items (full size %v), %d ops in %.1fs (%s) = %.1f ops/s\n",
		r.Created, r.FullSize, r.OpsTotal, dur.Seconds(), dur.Round(time.Second), r.OpsPerSecond)
	if c, ok := r.ClientLatency["claim_issued"]; ok {
		fmt.Fprintf(&b, "  claim (issued) client p50 %.2fms p99 %.2fms max %.2fms over %d; server: %s\n",
			c.P50MS, c.P99MS, c.MaxMS, c.Count, r.ServerLatency)
	}
	fmt.Fprintf(&b, "  reclaim: %d abandoned, %d reaped, p50 %.0fms p99 %.0fms max %.0fms (bound %.0fms)\n",
		r.Reclaim.Abandoned, r.Reclaim.Reaped, r.Reclaim.Latency.P50MS, r.Reclaim.Latency.P99MS,
		r.Reclaim.Latency.MaxMS, r.Reclaim.BoundMS)
	fmt.Fprintf(&b, "  residue: synthetic %d, non-synthetic deltas %v, core %s\n",
		r.Residue.FirstZero, r.Residue.SecondZeroDelta, r.Residue.ThirdZero.Status)
	names := make([]string, 0, len(r.Criteria))
	for name := range r.Criteria {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := r.Criteria[name]
		fmt.Fprintf(&b, "  %-22s %-14s %s\n", name, c.Status, c.Detail)
	}
	return b.String()
}

// anomalyTally counts unexpected but non-aborting outcomes.
type anomalyTally struct {
	mu      sync.Mutex
	counts  map[string]int
	samples []string
}

// addErr counts an anomaly and keeps the first error texts for diagnosis.
func (a *anomalyTally) addErr(name string, err error) {
	a.add(name)
	if err == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.samples) < 20 {
		a.samples = append(a.samples, name+": "+err.Error())
	}
}

func (a *anomalyTally) texts() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.samples...)
}

func (a *anomalyTally) add(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.counts == nil {
		a.counts = map[string]int{}
	}
	a.counts[name]++
}

func (a *anomalyTally) snapshot() map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]int{}
	for k, v := range a.counts {
		out[k] = v
	}
	return out
}

// pinTally counts commit-pin outcomes by operation and code.
type pinTally struct{ anomalyTally }

func (p *pinTally) add(op string, code parity.Code) { p.anomalyTally.add(op + "=" + string(code)) }
