// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

// liveCollectorInstanceFilter is the #9420 predicate both source-state readers
// apply: a row counts only while its collector instance is enabled and not
// deactivated, so rows a retired collector left behind cannot read as fresh.
const liveCollectorInstanceFilter = "EXISTS ( SELECT 1 FROM collector_instances AS instance " +
	"WHERE instance.instance_id = state.collector_instance_id AND instance.enabled AND instance.deactivated_at IS NULL )"

// TestReadinessSourceStateCandidatesIgnoreDeactivatedInstances pins the
// filter inside the vulnerability_source_state_candidates CTE, before its
// LIMIT, so a retired instance's rows neither count toward
// target_incomplete nor crowd live rows out of the 200-row window.
func TestReadinessSourceStateCandidatesIgnoreDeactivatedInstances(t *testing.T) {
	t.Parallel()

	start := strings.Index(ListReadinessQuery, "vulnerability_source_state_candidates AS (")
	end := strings.Index(ListReadinessQuery, "vulnerability_source_state AS (")
	if start < 0 || end <= start {
		t.Fatalf("vulnerability_source_state_candidates CTE not found in ListReadinessQuery")
	}
	cte := strings.Join(strings.Fields(ListReadinessQuery[start:end]), " ")
	filter := strings.Index(cte, liveCollectorInstanceFilter)
	if filter < 0 {
		t.Fatalf("candidates CTE lacks the live-instance filter %q:\n%s", liveCollectorInstanceFilter, cte)
	}
	if limit := strings.Index(cte, "LIMIT 200"); limit < filter {
		t.Fatalf("live-instance filter must precede LIMIT 200 in the candidates CTE:\n%s", cte)
	}
}
