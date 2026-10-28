// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

// liveCollectorInstanceFilter is the #9420 predicate every readiness source of
// vulnerability targets applies: a target counts only while its collector
// instance is enabled and not deactivated, so rows a retired collector left
// behind cannot read as fresh.
const liveCollectorInstanceFilter = "AND instance.enabled AND instance.deactivated_at IS NULL"

// TestReadinessSourceTargetsIgnoreDeactivatedInstances pins the live-instance
// filter on both target sources. Readiness reads source state only through
// those targets (a keyed lookup on the target's collector_instance_id and
// scope_id), so a retired instance's rows are never read, neither counting
// toward target_incomplete nor crowding live rows out of the 200-target window.
func TestReadinessSourceTargetsIgnoreDeactivatedInstances(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, start, end string }{
		{"configured targets", "configured_vulnerability_source_targets AS (", "target_vulnerability_source_scopes AS ("},
		{"derived config", "derived_vulnerability_source_config AS (", "derived_request_keys AS ("},
	} {
		start := strings.Index(ListReadinessQuery, tc.start)
		end := strings.Index(ListReadinessQuery, tc.end)
		if start < 0 || end <= start {
			t.Fatalf("%s: CTE bounds %q..%q not found in ListReadinessQuery", tc.name, tc.start, tc.end)
		}
		cte := strings.Join(strings.Fields(ListReadinessQuery[start:end]), " ")
		// The leading "AND " anchors the predicate as a conjunct, so a negated
		// form does not satisfy it.
		if !strings.Contains(cte, liveCollectorInstanceFilter) {
			t.Fatalf("%s lacks the live-instance filter %q:\n%s", tc.name, liveCollectorInstanceFilter, cte)
		}
	}

	_, tail, found := strings.Cut(ListReadinessQuery, "configured_vulnerability_source_state_candidates AS (")
	if !found {
		t.Fatal("configured_vulnerability_source_state_candidates CTE not found in ListReadinessQuery")
	}
	candidates := strings.Join(strings.Fields(tail), " ")
	if !strings.Contains(candidates, "WHERE state.collector_instance_id = target.collector_instance_id AND state.scope_id = target.scope_id") {
		t.Fatalf("configured state candidates must read state only through the live targets")
	}
}
