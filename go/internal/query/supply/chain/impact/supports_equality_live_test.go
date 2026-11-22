// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B1 disposable PostgreSQL 18 equality proof: the test-only candidate list read over the per-support table (filled by the marker-gated backfill from the multi-support corpus) returns the legacy read's page for every granted shape and every non-rank filter, and the ruled pick-then-filter page for every rank filter (compared with an independent oracle, nine differences from legacy pinned for B3), including image_ref and profile existence filters, a same-scope canonical-key collision, operator overlays (live, expired, invalid expiry, competing, superseded generation), and derived-class rows under a grant that covers their scope and one that does not; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
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
	overlay := func(factID, id, state, expires, generationID string, score int) {
		payload := groupedLiveFindingPayload(id, id, score, 5.0)
		payload["suppression_state"] = state
		payload["suppression"] = map[string]any{"expires_at": expires, "reason": "supports equality"}
		groupedLiveFact(t, ctx, database, factID, supportsOperatorScope, generationID, FindingFactKind, payload)
	}
	// The first operator generation is superseded below, so its overlays must
	// reach neither read: p1 would be hidden if one did. The overlays sit on keys
	// the rank filters do not split, so the divergent keys (m1, m2) stay visible.
	overlay("fact:op:old:p1", "p1", "false_positive", "2099-01-01T00:00:00Z", "gen:operator", 90)
	for _, statement := range []string{
		`UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE generation_id = 'gen:operator'`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('gen:operator:2', 'operator:vulnerability_suppressions', 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen:operator:2' WHERE scope_id = 'operator:vulnerability_suppressions'`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("supersede the operator generation: %v", err)
		}
	}
	overlay("fact:op:s1", "s1", "accepted_risk", "2099-01-01T00:00:00Z", "gen:operator:2", 50)
	overlay("fact:op:m3", "m3", "false_positive", "2020-01-01T00:00:00Z", "gen:operator:2", 50)
	// Two competing overrides for s2: the higher priority wins, whatever the state.
	overlay("fact:op:s2:low", "s2", "accepted_risk", "2099-01-01T00:00:00Z", "gen:operator:2", 40)
	overlay("fact:op:s2:high", "s2", "ignored", "2099-01-01T00:00:00Z", "gen:operator:2", 70)
	// An expiry the cast cannot read: the legacy read and the candidate must agree
	// on what it does, whatever that is.
	overlay("fact:op:m4", "m4", "accepted_risk", "not-a-timestamp", "gen:operator:2", 50)
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

// supportsRankOracle answers a rank-filter call by the ruled rule without the
// candidate: run the legacy read with the rank binds empty (the pick over
// granted supports that pass the key and existence filters, unpaged (the corpus holds fewer than 200 findings)), then
// filter the picked rows in Go. It shares no SQL with the candidate beyond the
// legacy read it is built on.
func supportsRankOracle(ctx context.Context, store PostgresFindingStore, filter FindingFilter) ([]FindingRow, error) {
	base := filter
	base.Severity, base.PriorityBucket, base.MinPriorityScore = "", "", 0
	base.Limit, base.AfterFindingID = 200, ""
	picked, err := store.ListSupplyChainImpactFindings(ctx, base)
	if err != nil {
		return nil, err
	}
	out := []FindingRow{}
	for _, row := range picked {
		if filter.Severity != "" && cvss.Bucket(row.CVSSScore) != filter.Severity {
			continue
		}
		if filter.PriorityBucket != "" && row.PriorityBucket != filter.PriorityBucket {
			continue
		}
		if filter.MinPriorityScore > 0 && row.PriorityScore < filter.MinPriorityScore {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// supportsRankDifferences lists, for B3, every grant and rank filter where the
// ruled order and the legacy filter-then-pick order return different pages. The
// list is pinned: a new difference or a vanished one fails the test, so the
// change in answers PR-B3 makes on purpose is exactly this list.
var supportsRankDifferences = map[string]string{
	"repositories m1 and m2/priority bucket low":                                      "legacy m1(p=40) m2(p=30) | ruled (empty)",
	"repositories m1 and m2/severity medium":                                          "legacy m1(p=40) m2(p=30) | ruled (empty)",
	"repositories m1 and m2/severity medium with profile precise":                     "legacy m1(p=40) m2(p=30) | ruled (empty)",
	"scope SA and SB/priority bucket low":                                             "legacy m1(p=40) m2(p=30) m3(p=60) m4(p=60) p1(p=50) p2(p=45) | ruled m3(p=60) m4(p=60) p1(p=50) p2(p=45)",
	"scope SA and SB/severity medium":                                                 "legacy c1(p=62) i1(p=75) m1(p=40) m2(p=30) m3(p=60) m4(p=60) p1(p=50) p2(p=45) | ruled c1(p=62) i1(p=75) m3(p=60) m4(p=60) p1(p=50) p2(p=45)",
	"scope SA and SB/severity medium with profile precise":                            "legacy c1(p=62) i1(p=75) m1(p=40) m2(p=30) m3(p=60) m4(p=60) p1(p=50) p2(p=30) | ruled c1(p=62) i1(p=75) m3(p=60) m4(p=60) p1(p=50) p2(p=30)",
	"scope SA plus repository m1 (arms overlap)/priority bucket low":                  "legacy m1(p=40) m3(p=60) m4(p=60) p1(p=50) p2(p=30) | ruled m3(p=60) m4(p=60) p1(p=50) p2(p=30)",
	"scope SA plus repository m1 (arms overlap)/severity medium":                      "legacy c1(p=62) i1(p=55) m1(p=40) m3(p=60) m4(p=60) p1(p=50) p2(p=30) | ruled c1(p=62) i1(p=55) m3(p=60) m4(p=60) p1(p=50) p2(p=30)",
	"scope SA plus repository m1 (arms overlap)/severity medium with profile precise": "legacy c1(p=62) i1(p=55) m1(p=40) m3(p=60) m4(p=60) p1(p=50) p2(p=30) | ruled c1(p=62) i1(p=55) m3(p=60) m4(p=60) p1(p=50) p2(p=30)",
}

// TestSupportsCandidateListEqualsLegacyLive is the equality gate of #10880
// PR-B1. For every granted shape and every non-rank filter the candidate read
// over the supports table returns the legacy read's page, row for row and
// payload for payload. Rank filters (severity, priority bucket, minimum
// priority) follow the ruled order, pick then filter, which the legacy read
// does not, so those cases compare against supportsRankOracle and each
// difference from legacy is listed. The grant shapes include a scope grant for
// the derived-image scope and one for an ordinary scope that holds a
// derived-class fact: neither read may return the derived fact, with or
// without a grant that covers its scope.
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
		{"operator scope and SA", nil, []string{supportsOperatorScope, multiSupportScopeA}},
		{"repository without findings", []string{"repository:none"}, nil},
	}
	filters := []struct {
		name   string
		filter FindingFilter
		rank   bool
	}{
		{"status", FindingFilter{Status: "affected_exact"}, false},
		{"image_ref a", FindingFilter{ImageRef: "registry.example/a:1"}, false},
		{"image_ref b", FindingFilter{ImageRef: "registry.example/b:2"}, false},
		{"image_ref absent", FindingFilter{ImageRef: "registry.example/none:0"}, false},
		{"profile precise", FindingFilter{DetectionProfile: ProfilePrecise}, false},
		{"profile comprehensive", FindingFilter{DetectionProfile: ProfileComprehensive}, false},
		{"cve", FindingFilter{CVEID: "CVE-2026-m1"}, false},
		{"advisory collision key", FindingFilter{AdvisoryID: "ADV-C1"}, false},
		{"include suppressed", FindingFilter{IncludeSuppressed: true}, false},
		{"suppression state", FindingFilter{SuppressionState: "accepted_risk"}, false},
		{"paged", FindingFilter{Limit: 3, Sort: "priority_score_desc"}, false},
		{"severity critical", FindingFilter{Severity: "critical"}, true},
		{"severity medium", FindingFilter{Severity: "medium"}, true},
		{"priority bucket critical", FindingFilter{PriorityBucket: "critical"}, true},
		{"priority bucket low", FindingFilter{PriorityBucket: "low"}, true},
		{"min priority 60", FindingFilter{MinPriorityScore: 60}, true},
		{"priority bucket low with image_ref b", FindingFilter{PriorityBucket: "low", ImageRef: "registry.example/b:2"}, true},
		{"severity medium with profile precise", FindingFilter{Severity: "medium", DetectionProfile: ProfilePrecise}, true},
	}
	compared, nonEmpty := 0, 0
	differences := map[string]string{}
	for _, grant := range grants {
		for _, tc := range filters {
			name := grant.name + "/" + tc.name
			t.Run(name, func(t *testing.T) {
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
				want := legacy
				if tc.rank {
					if want, err = supportsRankOracle(ctx, legacyStore, filter); err != nil {
						t.Fatalf("rank oracle: %v", err)
					}
					legacyJSON, _ := json.Marshal(legacy)
					wantJSON, _ := json.Marshal(want)
					if string(legacyJSON) != string(wantJSON) {
						differences[name] = "legacy " + multiSupportDescribe(legacy) + " | ruled " + multiSupportDescribe(want)
						if len(want) == 0 {
							differences[name] += "(empty)"
						}
					}
				}
				wantJSON, _ := json.Marshal(want)
				gotJSON, _ := json.Marshal(got)
				if string(wantJSON) != string(gotJSON) {
					t.Fatalf("candidate differs from its reference (rank=%v)\nreference: %s\ncandidate: %s",
						tc.rank, multiSupportDescribe(want), multiSupportDescribe(got))
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
	t.Logf("compared %d grant x filter cases, %d non-empty, %d rank cases differ from legacy", compared, nonEmpty, len(differences))
	for name, diff := range differences {
		t.Logf("B3 difference: %s: %s", name, diff)
	}
	if len(differences) != len(supportsRankDifferences) {
		t.Errorf("rank differences from legacy = %d, pinned list has %d: update supportsRankDifferences with the cases logged above", len(differences), len(supportsRankDifferences))
	}
	for name, diff := range differences {
		if supportsRankDifferences[name] != diff {
			t.Errorf("rank difference %q = %q, pinned %q", name, diff, supportsRankDifferences[name])
		}
	}
}
