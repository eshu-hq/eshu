// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The three symbol->runtime families (handles_route #5995, runs_in #6000,
// invokes_cloud_action #5997) share ONE backend-free extraction seam
// (reducer.ExtractSymbolRuntimeIntentRows) and ONE cassette/Odù
// (ifa.SymbolRuntimeFamilyOdu / ifa.LoadSymbolRuntimeFamilyOdu) -- this file
// holds the plumbing all three guards reuse, so a family-specific guard file
// only ever contains that family's own edge-set derivation.
//
// The three families bend the row->edge relationship three different ways
// (see each guard's own doc comment for the derivation): HANDLES_ROUTE's
// intent-level dedupe key includes http_method but its graph MERGE identity
// does not, so N intent rows can collapse to fewer edges; RUNS_IN's Cypher
// MATCH fans an unbounded (Repository)-[:DEFINES]->(Workload) with no LIMIT,
// so one intent row can fan OUT to N edges; INVOKES_CLOUD_ACTION is a direct
// 1:1 row-to-edge mapping. None of the three can be asserted by converting
// intent rows to ExpectedEdge one-for-one without accounting for its own
// bend -- a guard that did so would either report a spurious EXTRA
// (HANDLES_ROUTE, from same-identity multi-method rows) or miss real edges
// entirely (RUNS_IN, whose fan-out factor is invisible in the intent row).

// symbolRuntimeGuardGenerationID and symbolRuntimeGuardClock are the fixed,
// deterministic inputs this guard hands to
// reducer.ExtractSymbolRuntimeIntentRows. go/internal/ifa/AGENTS.md forbids
// wall-clock time inside Ifá derivation; neither value participates in the
// edge-identity comparison any of the three guards performs (mirrors
// repoDependencyGuardClock's role exactly).
const symbolRuntimeGuardGenerationID = "gen-ifa-symbol-runtime-guard"

var symbolRuntimeGuardClock = time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)

// symbolRuntimeUpsertRows runs the real, shared, backend-free extractor over
// the Odù's own facts and keeps only one domain's per-edge upsert rows,
// dropping both the other two domains' rows and this domain's own
// repo-wide refresh row (payload["action"] == "refresh") -- mirroring the
// production filterUpsertRows gate
// (go/internal/reducer/shared_projection_readiness.go:245-258) a live
// backend applies before ever reaching a write statement.
func symbolRuntimeUpsertRows(envelopes []facts.Envelope, domain string) []reducer.SharedProjectionIntentRow {
	all := reducer.ExtractSymbolRuntimeIntentRows(envelopes, symbolRuntimeGuardGenerationID, symbolRuntimeGuardClock)
	out := make([]reducer.SharedProjectionIntentRow, 0, len(all))
	for _, row := range all {
		if row.ProjectionDomain != domain {
			continue
		}
		if anyToStringValue(row.Payload["action"]) == "refresh" {
			continue
		}
		out = append(out, row)
	}
	return out
}

// singleRegistryEdgeType returns the one relationship type a single-type
// materialized-edge family registers, deriving it from
// cypher.MaterializedEdgeIdentityProperties's sibling registry
// (MaterializedEdgeDomainEdgeTypes) rather than a second hardcoded literal --
// so a registry rename cannot silently drift out of step with this package's
// own constant. All three symbol->runtime families are single-type today.
func singleRegistryEdgeType(family string, registry map[string]struct{}) (string, error) {
	if len(registry) != 1 {
		types := make([]string, 0, len(registry))
		for t := range registry {
			types = append(types, t)
		}
		sort.Strings(types)
		return "", fmt.Errorf("ifa: family %q registers %d relationship type(s) (%v), want exactly 1 for a single-type symbol-runtime guard", family, len(registry), types)
	}
	// len(registry) == 1 is guaranteed by the guard above, so this loop returns
	// on its first iteration. The panic is unreachable and exists so the
	// invariant is stated rather than implied by a `return "", nil` that reads
	// as a real success path returning an empty relationship type.
	for t := range registry {
		return t, nil
	}
	panic("unreachable: len(registry) == 1 was checked above")
}

// missingSymbolRuntimeExpectedTypes reports any registry-owned relationship
// type the expected-edge fixture never exercises. Shared across all three
// families (each is single-type today) rather than duplicated three times,
// mirroring missingShellExecExpectedTypes's role for its own family.
func missingSymbolRuntimeExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
	present := make(map[string]struct{}, len(expected))
	for _, edge := range expected {
		present[edge.RelationshipType] = struct{}{}
	}
	var missing []string
	for edgeType := range registry {
		if _, ok := present[edgeType]; !ok {
			missing = append(missing, edgeType)
		}
	}
	sort.Strings(missing)
	return missing
}

// compareSymbolRuntimeExpectedEdges reports the exact-set mismatch between
// the hand-derived expectation and what the extractor (plus, for
// HANDLES_ROUTE/RUNS_IN, the workload projection) actually produced, naming
// both directions -- mirroring compareShellExecExpectedEdges. MISSING means
// the derivation stopped producing an edge the graph is supposed to carry;
// EXTRA means it started producing one nobody derived, the direction a
// "contains all expected" check would miss. label names the family in the
// mismatch message (e.g. "handles route", "runs in", "invokes cloud
// action").
func compareSymbolRuntimeExpectedEdges(oduName, label string, expected, actual []ExpectedEdge) string {
	want := make(map[string]int, len(expected))
	got := make(map[string]int, len(actual))
	for _, edge := range expected {
		want[edge.Key()]++
	}
	for _, edge := range actual {
		got[edge.Key()]++
	}
	var missing, extra []string
	for key, count := range want {
		if got[key] < count {
			missing = append(missing, key)
		}
	}
	for key, count := range got {
		if count > want[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	return fmt.Sprintf("odù %q: %s edge set does not match %d hand-derived edge(s); MISSING: %s; EXTRA: %s",
		oduName, label, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "))
}
