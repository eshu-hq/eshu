// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"strings"
	"testing"
)

func TestListIncidentsQueryUsesSeparateIdentityLookups(t *testing.T) {
	branches := strings.Split(ListIncidentsQuery, "\nUNION ALL\n")
	if len(branches) != 2 {
		t.Fatalf("incident lookup has %d identity branches, want 2", len(branches))
	}

	for index, branch := range branches {
		for _, predicate := range []string{
			"FROM fact_records AS fact",
			"fact.source_system = $1",
			"scope.active_generation_id = fact.generation_id",
			"generation.status = 'active'",
			"($3 = '' OR fact.scope_id = $3)",
			"fact.fact_kind = 'incident.record'",
			"fact.is_tombstone = FALSE",
		} {
			if !strings.Contains(branch, predicate) {
				t.Errorf("identity branch %d lacks %q", index, predicate)
			}
		}
	}

	if !strings.Contains(branches[0], "fact.payload->>'provider_incident_id' = $2") {
		t.Error("provider identity branch lacks exact lookup")
	}
	if strings.Contains(branches[0], "fact.source_record_id = $2") {
		t.Error("provider identity branch includes fallback lookup")
	}
	if !strings.Contains(branches[1], "NULLIF(fact.payload->>'provider_incident_id', '') IS NULL") ||
		!strings.Contains(branches[1], "fact.source_record_id = $2") {
		t.Error("source-record branch lacks legacy fallback predicates")
	}
	if strings.Count(ListIncidentsQuery, "ORDER BY scope_id ASC, observed_at DESC, fact_id ASC") != 1 ||
		strings.Count(ListIncidentsQuery, "LIMIT $4") != 1 {
		t.Error("incident lookup needs one final ordering and limit")
	}
}
