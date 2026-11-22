// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B1 disposable PostgreSQL 18 equality proof: the test-only candidate list read over the per-support table (filled by the marker-gated backfill from the multi-support corpus) returns exactly the findings the legacy fact read returns for every granted shape and every filter, including image_ref and profile existence filters, a same-scope canonical-key collision, operator suppression overlays, and derived-class rows under a grant that covers their scope and one that does not; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const supportsOperatorScope = "operator:vulnerability_suppressions"

// seedSupportsEqualityExtras adds what the multi-support corpus lacks:
//
//   - i1: two supports whose image refs differ (an existence filter hits only
//     the support whose ref matches, and the picked support is the other one);
//   - c1/c2: two facts in one scope with different fact identities and one SQL
//     canonical key, because the key collapses the cve and advisory ids;
//   - operator overlays on m1 (accepted_risk, expires later) and m3
//     (false_positive, already expired), so the override probe and the expiry
//     rule run.
func seedSupportsEqualityExtras(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	groupedLiveScope(t, ctx, database, supportsOperatorScope, "gen:operator")
	image := func(factID, scope, generation, ref string, score int) {
		payload := groupedLiveFindingPayload("i1", "i1", score, 5.0)
		payload["image_ref"] = ref
		groupedLiveFact(t, ctx, database, factID, scope, generation, FindingFactKind, payload)
	}
	image("fact:i1:a", multiSupportScopeA, "gen:a", "registry.example/a:1", 55)
	image("fact:i1:b", multiSupportScopeB, "gen:b", "registry.example/b:2", 75)
	collision := func(factID, id, cve string) {
		payload := groupedLiveFindingPayload(id, "c", 62, 6.2)
		payload["cve_id"] = cve
		payload["advisory_id"] = "ADV-C1"
		payload["package_id"] = "pkg:npm/example-c@1.0.0"
		groupedLiveFact(t, ctx, database, factID, multiSupportScopeA, "gen:a", FindingFactKind, payload)
	}
	collision("fact:c1:a", "c1", "ADV-C1")
	collision("fact:c2:a", "c2", "")
	overlay := func(factID, id, state, expires string) {
		payload := groupedLiveFindingPayload(id, id, 50, 5.0)
		payload["suppression_state"] = state
		payload["suppression"] = map[string]any{"expires_at": expires, "reason": "supports equality"}
		groupedLiveFact(t, ctx, database, factID, supportsOperatorScope, "gen:operator", FindingFactKind, payload)
	}
	overlay("fact:op:m1", "m1", "accepted_risk", "2099-01-01T00:00:00Z")
	overlay("fact:op:m3", "m3", "false_positive", "2020-01-01T00:00:00Z")
}

// openSupportsEqualityDatabase seeds the multi-support corpus and the extras,
// fills the supports table with the real backfill, and rebuilds the winners.
func openSupportsEqualityDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, database := openMultiSupportDatabase(t)
	seedSupportsEqualityExtras(t, ctx, database)
	backfill := storagepostgres.SupplyChainImpactSupportsBackfill{DB: storagepostgres.SQLDB{DB: database}, ChunkSize: 3}
	for step := 0; ; step++ {
		report, err := backfill.RunStep(ctx)
		if err != nil {
			t.Fatalf("backfill step %d: %v", step, err)
		}
		if report.Complete {
			break
		}
		if step > 200 {
			t.Fatal("backfill did not complete")
		}
	}
	var supports, live int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports`).Scan(&supports); err != nil {
		t.Fatalf("count supports: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE fact_kind = $1 AND NOT is_tombstone`, FindingFactKind).Scan(&live); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if supports != live || supports == 0 {
		t.Fatalf("supports = %d, live finding facts = %d, want equal and non-zero after the backfill", supports, live)
	}
	return ctx, database
}

func supportsCandidateList(ctx context.Context, database *sql.DB, query string, filter FindingFilter) ([]FindingRow, error) {
	rows, err := database.QueryContext(ctx, query, multiSupportWinnersListArgs(ctx, filter)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []FindingRow{}
	for rows.Next() {
		var id, confidence string
		var payload []byte
		if err := rows.Scan(&id, &confidence, &payload); err != nil {
			return nil, err
		}
		row, err := DecodeFindingRow(id, confidence, payload)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// TestSupportsCandidateListEqualsLegacyLive is the equality gate of #10880
// PR-B1: for every granted shape and filter the candidate read over the
// supports table returns the legacy read's page, row for row and payload for
// payload. The candidate applies every filter to the supports before the pick,
// as the legacy read does; PR-B2 changes the rank filters to the canonical
// pick-then-filter order the arbiter ruled and rewrites these expectations on
// purpose. The grant shapes include a scope grant for the derived-image scope
// and one for an ordinary scope that holds a derived-class fact: neither read
// may return the derived fact, with or without a grant that covers its scope.
func TestSupportsCandidateListEqualsLegacyLive(t *testing.T) {
	ctx, database := openSupportsEqualityDatabase(t)
	candidate := supportsCandidateListQuery(t)
	legacyStore := PostgresFindingStore{DB: database, ReadFromWinners: false}

	grants := []struct {
		name                 string
		repositories, scopes []string
	}{
		{"scope SA", nil, []string{multiSupportScopeA}},
		{"scope SB", nil, []string{multiSupportScopeB}},
		{"scope SA and SB", nil, []string{multiSupportScopeA, multiSupportScopeB}},
		{"repositories m1 and m2", []string{"repository:m1", "repository:m2"}, nil},
		{"scope SA plus repository s2", []string{"repository:s2"}, []string{multiSupportScopeA}},
		{"scope SA plus repository m1 (arms overlap)", []string{"repository:m1"}, []string{multiSupportScopeA}},
		{"derived scope", nil, []string{multiSupportDerivedScope}},
		{"derived scope and SA", nil, []string{multiSupportDerivedScope, multiSupportScopeA}},
		{"operator scope", nil, []string{supportsOperatorScope}},
		{"repository without findings", []string{"repository:none"}, nil},
	}
	filters := []struct {
		name   string
		filter FindingFilter
	}{
		{"status", FindingFilter{Status: "affected_exact"}},
		{"image_ref a", FindingFilter{ImageRef: "registry.example/a:1"}},
		{"image_ref b", FindingFilter{ImageRef: "registry.example/b:2"}},
		{"image_ref absent", FindingFilter{ImageRef: "registry.example/none:0"}},
		{"profile precise", FindingFilter{DetectionProfile: ProfilePrecise}},
		{"profile comprehensive", FindingFilter{DetectionProfile: ProfileComprehensive}},
		{"severity critical", FindingFilter{Severity: "critical"}},
		{"severity medium", FindingFilter{Severity: "medium"}},
		{"priority bucket critical", FindingFilter{PriorityBucket: "critical"}},
		{"priority bucket low", FindingFilter{PriorityBucket: "low"}},
		{"min priority 60", FindingFilter{MinPriorityScore: 60}},
		{"cve", FindingFilter{CVEID: "CVE-2026-m1"}},
		{"advisory collision key", FindingFilter{AdvisoryID: "ADV-C1"}},
		{"include suppressed", FindingFilter{IncludeSuppressed: true}},
		{"suppression state", FindingFilter{SuppressionState: "accepted_risk"}},
		{"paged", FindingFilter{Limit: 3, Sort: "priority_score_desc"}},
	}
	compared, nonEmpty := 0, 0
	for _, grant := range grants {
		for _, tc := range filters {
			t.Run(grant.name+"/"+tc.name, func(t *testing.T) {
				filter := tc.filter
				filter.AllowedRepositoryIDs, filter.AllowedScopeIDs = grant.repositories, grant.scopes
				if filter.Limit == 0 {
					filter.Limit = 100
				}
				if filter.Status == "" {
					// The list requires at least one narrowing filter.
					filter.Status = "affected_exact"
				}
				legacy, err := legacyStore.ListSupplyChainImpactFindings(ctx, filter)
				if err != nil {
					t.Fatalf("legacy list: %v", err)
				}
				got, err := supportsCandidateList(ctx, database, candidate, filter)
				if err != nil {
					t.Fatalf("candidate list: %v", err)
				}
				legacyJSON, _ := json.Marshal(legacy)
				gotJSON, _ := json.Marshal(got)
				if string(legacyJSON) != string(gotJSON) {
					t.Fatalf("candidate differs from legacy\nlegacy:    %s\ncandidate: %s", multiSupportDescribe(legacy), multiSupportDescribe(got))
				}
				for _, row := range got {
					if strings.Contains(row.FindingID, "derived") {
						t.Fatalf("a derived-class finding reached a page: %s", row.FindingID)
					}
				}
				compared++
				if len(got) > 0 {
					nonEmpty++
				}
			})
		}
	}
	if want := len(grants) * len(filters); compared != want {
		t.Fatalf("compared %d cases, want %d", compared, want)
	}
	if nonEmpty < compared/3 {
		t.Fatalf("only %d of %d cases returned rows: the matrix proves little", nonEmpty, compared)
	}
	t.Logf("compared %d grant x filter cases, %d non-empty", compared, nonEmpty)
}
