// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

// The #9875 fleet acceptance gate classifier. The live harness in
// supply_chain_impact_fleet_gate_9875_live_test.go seeds an advisory, a KEV
// listing, and an owned package on a catalogued service, drives the real
// SupplyChainImpactHandler, the winners maintenance, and the grouped inventory
// HTTP route, and hands what it observed to fleetGateViolations. The
// classifier is pure so its RED/GREEN pair runs without a database
// (supply_chain_impact_fleet_gate_violations_9875_test.go) and the live gate
// plants real defects into the same function.

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// fleetGateBoundEnv overrides fleetGateDefaultBound with a Go duration.
const fleetGateBoundEnv = "ESHU_FLEET_GATE_MAX_ELAPSED"

// fleetGateDefaultBound is the default ceiling on source commit to rollup
// read for one seeded CVE on a disposable PostgreSQL 18 database. Measured
// locally the three stages take well under one second together (see
// docs/public/reference/local-testing/fleet-acceptance-gate.md); the default
// leaves headroom for a loaded CI runner while still failing a stage that
// stalls, retries in a loop, or waits on a lease. It is a local pipeline
// bound, not the fleet freshness SLO: the per-source publish-to-finding bound
// for QA is set by the arbiter on #9875 and passed through fleetGateBoundEnv.
const fleetGateDefaultBound = 30 * time.Second

// fleetGateService is the catalogued service the gate expects the finding to
// reach.
const fleetGateService = "service:fleet-9875"

// fleetGateBucket is one decoded bucket of the grouped inventory response
// (GET /api/v0/supply-chain/impact/inventory?group_by=service_id).
type fleetGateBucket struct {
	Value            string         `json:"value"`
	Unattributed     bool           `json:"unattributed"`
	Count            int            `json:"count"`
	ByImpactStatus   map[string]int `json:"by_impact_status"`
	MaxPriorityScore int            `json:"max_priority_score"`
	Completeness     string         `json:"completeness"`
	MissingEvidence  []string       `json:"missing_evidence"`
}

// fleetGateWinner is the surfaced canonical finding for the seeded CVE on the
// service's repository, read from the winners-sourced findings store.
type fleetGateWinner struct {
	KnownExploited      bool
	PriorityScore       int
	PriorityBucket      string
	PriorityReasonCodes []string
	ServiceIDs          []string
}

// fleetGateStamps are the test-side wall-clock stamps of each stage. No
// source-commit or finding-written clock is stamped on the production path
// today, so the harness stamps time.Now() after each stage returns:
// SourceCommitted after the seeded facts and their work items commit (it
// stands in for the upstream publish being collected), FindingWritten after
// the reducer queue drains, WinnersBuilt after the keyed winners tick
// commits, and RollupRead after the grouped route answers.
type fleetGateStamps struct {
	SourceCommitted time.Time
	FindingWritten  time.Time
	WinnersBuilt    time.Time
	RollupRead      time.Time
}

// fleetGateStage is one named elapsed duration of the gate.
type fleetGateStage struct {
	Name    string
	Elapsed time.Duration
}

// stages returns the per-stage elapsed times, each measured from the previous
// stamp, followed by the source-commit-to-rollup-read total.
func (s fleetGateStamps) stages() []fleetGateStage {
	return []fleetGateStage{
		{"finding_written", s.FindingWritten.Sub(s.SourceCommitted)},
		{"winners_built", s.WinnersBuilt.Sub(s.FindingWritten)},
		{"rollup_read", s.RollupRead.Sub(s.WinnersBuilt)},
		{"total", s.RollupRead.Sub(s.SourceCommitted)},
	}
}

// fleetGateObservation is everything one gate run read back.
type fleetGateObservation struct {
	// RollupStatus is the HTTP status of the group_by=service_id read.
	RollupStatus     int
	Buckets          []fleetGateBucket
	DistinctFindings int
	// TierStatus and ServiceTiers come from the group_by=priority_bucket
	// read filtered to the expected service.
	TierStatus   int
	ServiceTiers map[string]int
	// Winner is nil when no canonical finding surfaced for the CVE.
	Winner *fleetGateWinner
	Stamps fleetGateStamps
}

// fleetGateExpectation is what a passing run must show.
type fleetGateExpectation struct {
	ServiceID         string
	ImpactStatus      string
	PriorityTier      string
	UnattributedCount int
	DistinctFindings  int
	Bound             time.Duration
}

// fleetGateViolations returns every way the observation misses the
// expectation, each prefixed with a stable code ("service_absent: ..."). An
// empty result is the gate passing.
func fleetGateViolations(o fleetGateObservation, e fleetGateExpectation) []string {
	var out []string
	add := func(code, format string, args ...any) {
		out = append(out, code+": "+fmt.Sprintf(format, args...))
	}

	if o.RollupStatus != http.StatusOK {
		add("rollup_unavailable", "group_by=service_id answered HTTP %d", o.RollupStatus)
	}
	var service, unattributed *fleetGateBucket
	for i := range o.Buckets {
		switch {
		case o.Buckets[i].Unattributed:
			unattributed = &o.Buckets[i]
		case o.Buckets[i].Value == e.ServiceID:
			service = &o.Buckets[i]
		}
	}
	if service == nil {
		add("service_absent", "no %s bucket in the service rollup (buckets %s)", e.ServiceID, fleetGateBucketSummary(o.Buckets))
	} else {
		if service.Completeness != "complete" {
			add("service_completeness", "%s completeness = %q, want complete", e.ServiceID, service.Completeness)
		}
		if service.ByImpactStatus[e.ImpactStatus] < 1 {
			add("service_impact_status", "%s by_impact_status = %v, want %s", e.ServiceID, service.ByImpactStatus, e.ImpactStatus)
		}
		if o.Winner != nil && service.MaxPriorityScore != o.Winner.PriorityScore {
			add("service_priority", "%s max_priority_score = %d, want the KEV-scored winner's %d",
				e.ServiceID, service.MaxPriorityScore, o.Winner.PriorityScore)
		}
	}

	if o.Winner == nil {
		add("winner_absent", "no canonical finding surfaced for the seeded CVE")
	} else {
		if !o.Winner.KnownExploited || !slices.Contains(o.Winner.PriorityReasonCodes, "cisa_kev") {
			add("kev_missing", "winner known_exploited = %t, reason codes %q, want true with cisa_kev",
				o.Winner.KnownExploited, o.Winner.PriorityReasonCodes)
		}
		// The reducer attributes the service at reduce time; the rollup
		// resolves it at read time. The two must agree.
		if !slices.Contains(o.Winner.ServiceIDs, e.ServiceID) {
			add("winner_service", "winner service_ids = %q, want %s", o.Winner.ServiceIDs, e.ServiceID)
		}
		if o.Winner.PriorityBucket != e.PriorityTier {
			add("priority_tier", "winner priority_bucket = %q (score %d), want %q",
				o.Winner.PriorityBucket, o.Winner.PriorityScore, e.PriorityTier)
		}
	}
	if o.TierStatus != http.StatusOK {
		add("priority_tier", "group_by=priority_bucket&service_id=%s answered HTTP %d", e.ServiceID, o.TierStatus)
	} else if o.ServiceTiers[e.PriorityTier] < 1 {
		add("priority_tier", "%s priority tiers = %v, want at least one %s", e.ServiceID, o.ServiceTiers, e.PriorityTier)
	}

	switch {
	case unattributed == nil:
		add("unattributed_bucket", "no unattributed bucket reported (buckets %s)", fleetGateBucketSummary(o.Buckets))
	case unattributed.Count != e.UnattributedCount || unattributed.Completeness != "unattributed" ||
		!slices.Contains(unattributed.MissingEvidence, "service_mapping"):
		add("unattributed_bucket", "unattributed bucket count %d, completeness %q, missing %q; want %d, unattributed, service_mapping",
			unattributed.Count, unattributed.Completeness, unattributed.MissingEvidence, e.UnattributedCount)
	}
	if o.DistinctFindings != e.DistinctFindings {
		add("distinct_findings", "distinct_findings = %d, want %d", o.DistinctFindings, e.DistinctFindings)
	}

	out = append(out, fleetGateTimingViolations(o.Stamps, e.Bound)...)
	return out
}

// fleetGateTimingViolations checks the stamps are present and ordered and the
// total stays within bound. A non-positive bound is itself a violation, so a
// misconfigured gate cannot pass by checking nothing.
func fleetGateTimingViolations(s fleetGateStamps, bound time.Duration) []string {
	stamps := []time.Time{s.SourceCommitted, s.FindingWritten, s.WinnersBuilt, s.RollupRead}
	for i, stamp := range stamps {
		if stamp.IsZero() || (i > 0 && stamp.Before(stamps[i-1])) {
			return []string{fmt.Sprintf("stage_order: stage stamps %v are missing or out of order", stamps)}
		}
	}
	if bound <= 0 {
		return []string{fmt.Sprintf("elapsed_bound: bound %v is not positive", bound)}
	}
	if total := s.RollupRead.Sub(s.SourceCommitted); total > bound {
		return []string{fmt.Sprintf("elapsed_bound: source commit to rollup read took %v, over the %v bound (stages %v)", total, bound, s.stages())}
	}
	return nil
}

// fleetGateBoundFrom parses the fleetGateBoundEnv value: blank takes the
// default, anything else must be a positive Go duration.
func fleetGateBoundFrom(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fleetGateDefaultBound, nil
	}
	bound, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: %w", fleetGateBoundEnv, raw, err)
	}
	if bound <= 0 {
		return 0, fmt.Errorf("%s=%q must be positive", fleetGateBoundEnv, raw)
	}
	return bound, nil
}

func fleetGateBucketSummary(buckets []fleetGateBucket) string {
	parts := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		parts = append(parts, fmt.Sprintf("%q=%d/%s", bucket.Value, bucket.Count, bucket.Completeness))
	}
	return "[" + strings.Join(parts, " ") + "]"
}
