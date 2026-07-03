// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// observabilityCoverageEdgeTally is the bounded, honest accounting surface for
// the COVERS edge projection (issue #391 PR3, mirroring the #805 §6 tally). The
// metric and completion log read materialized vs skipped counts keyed by the
// closed coverage-signal vocabulary, so cardinality stays bounded.
type observabilityCoverageEdgeTally struct {
	// materialized counts COVERS edges written, keyed by coverage signal
	// (alarm / composite_alarm / dashboard / log_group / trace_sampling).
	materialized map[string]int
	// skipped counts derived coverage decisions that did NOT produce an edge
	// because they resolved no target CloudResource uid (e.g. X-Ray service
	// coverage), keyed by coverage signal. Counted, never silently dropped.
	// Only derived qualifies: the classifier guarantees an exact decision always
	// carries a non-empty TargetUID (so exact is never skipped), and
	// ambiguous/unresolved/stale/rejected are gaps or drift, not reachable
	// coverage. This matches totalSkipped and the completion-log accounting.
	skipped map[string]int
}

func newObservabilityCoverageEdgeTally() observabilityCoverageEdgeTally {
	return observabilityCoverageEdgeTally{
		materialized: make(map[string]int),
		skipped:      make(map[string]int),
	}
}

// ExtractObservabilityCoverageEdgeRows builds canonical COVERS edge rows from
// the scope generation's facts. It runs the same pure classifier the PR1
// fact-only read model uses (BuildObservabilityCoverageDecisions) and promotes
// to a graph edge ONLY the decisions that resolved an observability object to a
// monitored target by a stable CloudResource uid — i.e. exact coverage with a
// non-empty target uid. Provenance-only outcomes (derived/ambiguous/unresolved/
// stale/rejected) never fabricate an edge: ambiguous matched multiple uids and
// picks none, X-Ray derived coverage resolves a service name with no target
// uid, and gap/stale/rejected carry no covered target. This is the
// no-fabrication contract from #805 §5.3 and the memo §5 truth matrix.
//
// Rows are deduplicated by (observability_uid, coverage_signal, target_uid) and
// sorted deterministically so the batched write is stable across retries and
// reprojections.
func ExtractObservabilityCoverageEdgeRows(
	envelopes []facts.Envelope,
) ([]map[string]any, observabilityCoverageEdgeTally, error) {
	tally := newObservabilityCoverageEdgeTally()
	decisions, err := BuildObservabilityCoverageDecisions(envelopes)
	if err != nil {
		return nil, tally, err
	}
	if len(decisions) == 0 {
		return nil, tally, nil
	}

	type edgeKey struct {
		observability string
		signal        string
		target        string
	}
	seen := make(map[edgeKey]struct{}, len(decisions))
	rows := make([]map[string]any, 0, len(decisions))

	for _, decision := range decisions {
		if !coverageDecisionIsEdgeEligible(decision) {
			if decision.Outcome == ObservabilityCoverageDerived {
				// Derived coverage is real but resolves no target uid (X-Ray
				// service-name coverage). Count it as skipped so the operator can
				// see coverage that the read model knows about but the graph
				// cannot reach yet, rather than dropping it silently.
				tally.skipped[decision.CoverageSignal]++
			}
			continue
		}
		key := edgeKey{
			observability: decision.ObservabilityUID,
			signal:        decision.CoverageSignal,
			target:        decision.TargetUID,
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		tally.materialized[decision.CoverageSignal]++
		rows = append(rows, map[string]any{
			"observability_uid": decision.ObservabilityUID,
			"target_uid":        decision.TargetUID,
			"coverage_signal":   decision.CoverageSignal,
			"resolution_mode":   decision.ResolutionMode,
		})
	}

	if len(rows) == 0 {
		return nil, tally, nil
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["coverage_signal"]) + ":" +
			anyToString(rows[a]["observability_uid"]) + "->" +
			anyToString(rows[a]["target_uid"])
		right := anyToString(rows[b]["coverage_signal"]) + ":" +
			anyToString(rows[b]["observability_uid"]) + "->" +
			anyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows, tally, nil
}

// coverageDecisionIsEdgeEligible reports whether a coverage decision proves a
// canonical COVERS edge. Only an exact match that resolved both a covering
// observability uid and a covered target uid is eligible; everything else stays
// provenance-only and produces no edge.
func coverageDecisionIsEdgeEligible(decision ObservabilityCoverageCorrelationDecision) bool {
	return decision.Outcome == ObservabilityCoverageExact &&
		decision.ObservabilityUID != "" &&
		decision.TargetUID != ""
}
