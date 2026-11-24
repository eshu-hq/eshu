// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-C1 f disposable PostgreSQL 18 proof that the decide-first ungranted selector explain answers exactly what the legacy explain answers: zero, one and several canonical keys, a key whose only copies are unauthorized, a key whose best-ranked copy is unauthorized, a scanner-associated copy, a derived-image scope, an operator-suppressed finding and a retracted scanner classification; and that statements with the authority probe or the identity predicate removed diverge on the same corpus
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

const (
	decideScopeStale    = "scope:decide-stale"
	decideGenStaleOld   = "gen:decide-stale-old"
	decideGenStaleNew   = "gen:decide-stale-new"
	decideDerivedScope  = "image://derived/decide"
	decideDerivedGen    = "gen:decide-derived"
	decideRepository    = "repository:decide"
	decideOperatorState = "false_positive"
)

// decidePayload is one fixture finding payload. The canonical key folds the CVE, package and
// repository, so copies of one finding in several scopes share a key.
func decidePayload(cve, pkg string, score int) map[string]any {
	payload := groupedLiveFindingPayload("decide", "decide", score, 5.0)
	payload["finding_id"] = "finding:decide:" + cve + ":" + pkg
	payload["cve_id"] = "CVE-2026-" + cve
	payload["package_id"] = "pkg:npm/" + pkg + "@1.0.0"
	payload["repository_id"] = decideRepository
	return payload
}

// seedDecideFirstFixtures adds the cases the multi-support corpus lacks: every one is a
// canonical key whose copies the authority probe must treat in a specific way.
func seedDecideFirstFixtures(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	groupedLiveScope(t, ctx, database, decideScopeStale, decideGenStaleNew)
	groupedLiveScope(t, ctx, database, decideDerivedScope, decideDerivedGen)
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'superseded', NOW(), '{}'::jsonb)`, decideGenStaleOld, decideScopeStale); err != nil {
		t.Fatalf("insert the superseded generation: %v", err)
	}
	fact := func(id, scope, generation, cve, pkg string, score int) {
		groupedLiveFact(t, ctx, database, id, scope, generation, FindingFactKind, decidePayload(cve, pkg, score))
	}
	// d1: one authorized key (k1) beside a key with only stale copies (k2), a derived-scope key (k3),
	// a scanner-associated key (k4) and a derived-classified key (k5). k1's best-ranked copy is the stale one.
	fact("fact:d1:k1:stale", decideScopeStale, decideGenStaleOld, "d1", "k1", 95)
	fact("fact:d1:k1:a", multiSupportScopeA, "gen:a", "d1", "k1", 60)
	fact("fact:d1:k2:stale", decideScopeStale, decideGenStaleOld, "d1", "k2", 80)
	fact("fact:d1:k3:derived", decideDerivedScope, decideDerivedGen, "d1", "k3", 80)
	fact("fact:d1:k4:assoc", multiSupportScopeA, "gen:a", "d1", "k4", 80)
	fact("fact:d1:k5:class", multiSupportScopeA, "gen:a", "d1", "k5", 80)
	// d2: two authorized keys, one of them with a static active classification, beside a retracted one.
	fact("fact:d2:k6:static", multiSupportScopeA, "gen:a", "d2", "k6", 70)
	fact("fact:d2:k7:b", multiSupportScopeB, "gen:b", "d2", "k7", 70)
	fact("fact:d2:k8:retracted", multiSupportScopeA, "gen:a", "d2", "k8", 70)
	// d3: two authorized keys, one operator-suppressed: a suppressed winner still counts.
	fact("fact:d3:k9:a", multiSupportScopeA, "gen:a", "d3", "k9", 70)
	fact("fact:d3:k10:b", multiSupportScopeB, "gen:b", "d3", "k10", 70)
	// d4: one authorized key, operator-suppressed.
	fact("fact:d4:k11:a", multiSupportScopeA, "gen:a", "d4", "k11", 70)
	// d5: only stale copies: not found. d6: one key with copies in two scopes: found, the better copy wins.
	fact("fact:d5:k12:stale", decideScopeStale, decideGenStaleOld, "d5", "k12", 70)
	fact("fact:d6:k13:a", multiSupportScopeA, "gen:a", "d6", "k13", 50)
	fact("fact:d6:k13:b", multiSupportScopeB, "gen:b", "d6", "k13", 75)
	for _, overlay := range []struct{ id, cve, pkg string }{
		{"fact:op:d3:k10", "d3", "k10"}, {"fact:op:d4:k11", "d4", "k11"},
	} {
		payload := decidePayload(overlay.cve, overlay.pkg, 90)
		payload["suppression_state"] = decideOperatorState
		payload["suppression"] = map[string]any{"expires_at": "2099-01-01T00:00:00Z", "reason": "decide-first"}
		groupedLiveFact(t, ctx, database, overlay.id, supportsOperatorScope, "gen:operator:2", FindingFactKind, payload)
	}
	for _, statement := range []string{
		`INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle, scan_scope_id) VALUES ('fact:d1:k5:class', 'derived', 'active', '')`,
		`INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle, scan_scope_id) VALUES ('fact:d2:k6:static', 'static', 'active', '')`,
		`INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle, scan_scope_id) VALUES ('fact:d2:k8:retracted', 'static', 'retracted', '')`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("insert scanner classification: %v", err)
		}
	}
	// The association row's foreign keys are bypassed for this fixture only: the read guard looks
	// at the row's presence, never at its publication.
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatalf("relax foreign keys for the association fixture: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO scanner_image_result_fact_associations (finding_fact_id, publication_id, scan_scope_id, scan_generation_id, tenant_id, workspace_id)
VALUES ('fact:d1:k4:assoc', 'publication:decide', 'scan-scope:decide', 'gen', '', '')`); err != nil {
		t.Fatalf("insert scanner association: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit association fixture: %v", err)
	}
}

// decideOutcome is explainOutcome with the ambiguous count, which the refusal envelope carries.
func decideOutcome(row ExplanationRow, err error) string {
	if errors.Is(err, ErrExplanationAmbiguous) {
		return fmt.Sprintf("ambiguous(%d)", ExplanationAmbiguousCandidateCount(err))
	}
	if err == nil {
		encoded, _ := json.Marshal(row)
		return string(encoded)
	}
	return explainOutcome(row, err)
}

// decideRequests are the ungranted explain requests the matrix runs: every selector a bounded
// scope allows, over every fixture CVE, plus the identity lookups.
func decideRequests(all []FindingRow) []packet.ExplanationFilter {
	requests := []packet.ExplanationFilter{}
	for _, cve := range []string{"d1", "d2", "d3", "d4", "d5", "d6"} {
		requests = append(requests,
			packet.ExplanationFilter{CVEID: "CVE-2026-" + cve, RepositoryID: decideRepository},
			packet.ExplanationFilter{AdvisoryID: "CVE-2026-" + cve, RepositoryID: decideRepository},
		)
	}
	for _, fixture := range []struct{ cve, pkg string }{
		{"d1", "k1"},
		{"d1", "k2"},
		{"d1", "k3"},
		{"d1", "k4"},
		{"d1", "k5"},
		{"d2", "k6"},
		{"d2", "k7"},
		{"d2", "k8"},
		{"d3", "k9"},
		{"d3", "k10"},
		{"d4", "k11"},
		{"d5", "k12"},
		{"d6", "k13"},
	} {
		requests = append(requests, packet.ExplanationFilter{CVEID: "CVE-2026-" + fixture.cve, PackageID: "pkg:npm/" + fixture.pkg + "@1.0.0"})
	}
	for _, row := range all {
		requests = append(requests,
			packet.ExplanationFilter{CVEID: row.CVEID, PackageID: row.PackageID},
			packet.ExplanationFilter{CVEID: row.CVEID, RepositoryID: row.RepositoryID},
			packet.ExplanationFilter{FindingID: row.FindingID},
			packet.ExplanationFilter{FindingID: row.FindingID, CVEID: row.CVEID, RepositoryID: row.RepositoryID},
		)
	}
	// A fact id the public-id lookup misses falls through to the selector statement with the identity
	// predicate: it must narrow to that fact's canonical key, whatever else the selector matches.
	for _, fixture := range []struct{ fact, cve string }{
		{"fact:d1:k1:a", "d1"},
		{"fact:d2:k6:static", "d2"},
		{"fact:d2:k7:b", "d2"},
		{"fact:d3:k9:a", "d3"},
		{"fact:d3:k10:b", "d3"},
		{"fact:d6:k13:b", "d6"},
		{"fact:d1:k2:stale", "d1"},
	} {
		requests = append(requests, packet.ExplanationFilter{FindingID: fixture.fact, CVEID: "CVE-2026-" + fixture.cve, RepositoryID: decideRepository})
	}
	for _, id := range []string{"d1:k1", "d1:k2", "d2:k6", "d3:k10", "d4:k11", "d6:k13", "missing"} {
		requests = append(requests, packet.ExplanationFilter{FindingID: "finding:decide:" + id})
	}
	return requests
}

// decideCompare runs every request through both stores and returns the mismatches plus counts of each
// outcome class, so a caller can require the matrix to be non-vacuous.
func decideCompare(ctx context.Context, requests []packet.ExplanationFilter, candidate, legacy PostgresFindingStore) (mismatches []string, found, ambiguous, notFound int) {
	for _, request := range requests {
		got, gotErr := candidate.ExplainSupplyChainImpact(ctx, request)
		want, wantErr := legacy.ExplainSupplyChainImpact(ctx, request)
		if decideOutcome(got, gotErr) != decideOutcome(want, wantErr) {
			mismatches = append(mismatches, fmt.Sprintf("%+v\n  legacy:   %s\n  decide-first: %s", request, decideOutcome(want, wantErr), decideOutcome(got, gotErr)))
		}
		switch {
		case errors.Is(wantErr, ErrExplanationNotFound):
			notFound++
		case errors.Is(wantErr, ErrExplanationAmbiguous):
			ambiguous++
		case wantErr == nil:
			found++
		}
	}
	return mismatches, found, ambiguous, notFound
}

// TestExplainUngrantedDecideFirstEqualsLegacyLive holds the decide-first ungranted explain to the
// legacy explain over zero, one and several canonical keys and the authority cases of the
// fixtures; a seeded-violation subtest removes the authority probe, the second-key compare and the
// identity predicate in turn and requires the same matrix to catch each.
func TestExplainUngrantedDecideFirstEqualsLegacyLive(t *testing.T) {
	ctx, database := openSupportsEqualityDatabaseWith(t, seedDecideFirstFixtures)
	candidate := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}
	all, err := legacy.ListSupplyChainImpactFindings(ctx, FindingFilter{Status: "affected_exact", Limit: 200})
	if err != nil || len(all) == 0 {
		t.Fatalf("seed list = %d rows, %v", len(all), err)
	}
	requests := decideRequests(all)

	t.Run("equals legacy", func(t *testing.T) {
		mismatches, found, ambiguous, notFound := decideCompare(ctx, requests, candidate, legacy)
		if len(mismatches) > 0 {
			t.Fatalf("%d of %d requests differ from legacy:\n%s", len(mismatches), len(requests), strings.Join(mismatches, "\n"))
		}
		t.Logf("compared %d requests: %d found, %d ambiguous, %d not found", len(requests), found, ambiguous, notFound)
		if found == 0 || ambiguous == 0 || notFound == 0 {
			t.Fatalf("matrix is vacuous: found %d, ambiguous %d, not found %d; it needs all three", found, ambiguous, notFound)
		}
	})

	original := map[explainDrivingKey]string{}
	for key, query := range ExplainFindingUngrantedByKeyQuery {
		original[key] = query
	}
	mutate := func(t *testing.T, edit func(string) string) {
		t.Helper()
		t.Cleanup(func() {
			for key, query := range original {
				ExplainFindingUngrantedByKeyQuery[key] = query
			}
		})
		for key, query := range original {
			changed := edit(query)
			if changed == query {
				t.Fatalf("the seeded violation changed nothing in the %s statement", key)
			}
			ExplainFindingUngrantedByKeyQuery[key] = changed
		}
	}
	diverges := func(t *testing.T) {
		t.Helper()
		mismatches, _, _, _ := decideCompare(ctx, requests, candidate, legacy)
		if len(mismatches) == 0 {
			t.Fatal("the matrix did not catch the seeded violation: a regression here would ship")
		}
	}
	t.Run("seeded violation: no authority probe", func(t *testing.T) {
		mutate(t, func(q string) string {
			q = strings.ReplaceAll(q, "AND scope.active_generation_id = fact.generation_id\n", "AND TRUE\n")
			return strings.ReplaceAll(q, "AND generation.status = 'active'\n", "AND TRUE\n")
		})
		diverges(t)
	})
	t.Run("seeded violation: no scanner association guard", func(t *testing.T) {
		mutate(t, func(q string) string {
			return strings.ReplaceAll(q, "AND finding_association.finding_fact_id IS NULL\n", "AND TRUE\n")
		})
		diverges(t)
	})
	t.Run("seeded violation: no second-key compare", func(t *testing.T) {
		mutate(t, func(q string) string {
			return strings.ReplaceAll(q, "WHERE fact.canonical_key <> (SELECT first_key.canonical_key FROM first_hit AS first_key)\n    AND ", "WHERE ")
		})
		diverges(t)
	})
	t.Run("seeded violation: no identity predicate", func(t *testing.T) {
		mutate(t, func(q string) string {
			return strings.ReplaceAll(q, "($2 = ''\n        OR fact.fact_id = $2", "(TRUE\n        OR fact.fact_id = $2")
		})
		diverges(t)
	})
}
