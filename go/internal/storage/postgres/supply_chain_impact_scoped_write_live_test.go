// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// scopedLiveWrite runs one real writer pass on one scope.
func scopedLiveWrite(
	t *testing.T,
	ctx context.Context,
	writer supplychaincore.PostgresSupplyChainImpactWriter,
	token *int64,
	scope, generation string,
	scoped []string,
	cves ...string,
) supplychaincore.SupplyChainImpactWriteResult {
	t.Helper()
	*token++
	findings := make([]supplychaincore.SupplyChainImpactFinding, 0, len(cves))
	for _, cve := range cves {
		finding := fanOutLiveFinding(cve)
		if cve == "CVE-SCOPED-CHANGED" {
			finding.ObservedVersion = "2.0.0"
		}
		findings = append(findings, finding)
	}
	result, err := writer.WriteSupplyChainImpactFindings(ctx, supplychaincore.SupplyChainImpactWrite{
		IntentID: fmt.Sprintf("intent-%d", *token), ScopeID: scope,
		GenerationID: fanOutGen(scope, generation),
		SourceSystem: "synthetic", Cause: "differential", Findings: findings,
		FencingToken: *token, ScopedCVEIDs: scoped,
	})
	if err != nil {
		t.Fatalf("write %s/%s scoped=%v: %v", scope, generation, scoped, err)
	}
	return result
}

// diffLiveRows reads the active finding rows of one generation as payloads
// with the pass and scope identity redacted: the two halves run on different
// scopes, so scope_id, generation_id, intent_id, cause, and the digest that
// covers them differ by construction. The differential compares everything
// else: the finding's asserted truth.
func diffLiveRows(t *testing.T, ctx context.Context, db *sql.DB, scope, generation string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT payload->>'cve_id', ((payload - 'intent_id' - 'cause' - 'scope_id' - 'generation_id') #- '{provenance,content_digest}')::text
FROM fact_records
WHERE fact_kind = '`+facts.ReducerSupplyChainImpactFindingFactKind+`'
  AND scope_id = $1 AND generation_id = $2 AND is_tombstone = FALSE
ORDER BY 1, 2`, scope, fanOutGen(scope, generation))
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var cve, payload string
		if err := rows.Scan(&cve, &payload); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		out[cve+"|"+payload] = payload
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows: %v", err)
	}
	return out
}

// diffLiveRowCount counts the active finding rows of one generation for one CVE.
func diffLiveRowCount(t *testing.T, ctx context.Context, db *sql.DB, scope, generation, cve string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_records
WHERE fact_kind = '`+facts.ReducerSupplyChainImpactFindingFactKind+`'
  AND scope_id = $1 AND generation_id = $2 AND is_tombstone = FALSE
  AND payload->>'cve_id' = $3`, scope, fanOutGen(scope, generation), cve).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// TestScopedWriteMatchesFullWriteLive is the #9779 row differential: over two
// identical scopes, a full pass and a scoped pass for one CVE write identical
// rows for that CVE (modulo pass identity). A stale row inside the stamp is
// retracted by both; a stale row outside the stamp is retracted by the full
// pass only — the scoped pass leaves it byte-identical.
func TestScopedWriteMatchesFullWriteLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
	}
	var token int64
	baseline := []string{"CVE-SCOPED-CHANGED", "CVE-OUTSIDE"}

	for _, scope := range []string{"diff-full", "diff-scoped"} {
		fanOutSeedScope(t, ctx, db, scope, "g1")
		scopedLiveWrite(t, ctx, writer, &token, scope, "g1", nil, baseline...)
		// A stale row per CVE the next pass no longer derives: the full pass
		// retracts both, the scoped pass only the stamped one.
		for _, cve := range baseline {
			seedScopedFindingRow(t, ctx, db, scope, "g1", "stale-"+scope+"-"+cve,
				cve, cve, "pkg:stale", "pkg:npm/stale@1", "stale", "npm", false)
		}
	}

	full := scopedLiveWrite(t, ctx, writer, &token, "diff-full", "g1", nil, baseline...)
	scoped := scopedLiveWrite(t, ctx, writer, &token, "diff-scoped", "g1",
		[]string{"CVE-SCOPED-CHANGED"}, baseline...)

	if !reflect.DeepEqual(full.DerivedCVEIDs, []string{"CVE-OUTSIDE", "CVE-SCOPED-CHANGED"}) {
		t.Fatalf("full derived = %v, want both CVEs", full.DerivedCVEIDs)
	}
	if !reflect.DeepEqual(scoped.DerivedCVEIDs, []string{"CVE-SCOPED-CHANGED"}) {
		t.Fatalf("scoped derived = %v, want the stamped row plus its retracted stale copy", scoped.DerivedCVEIDs)
	}
	if !reflect.DeepEqual(scoped.ScopedCVEIDs, []string{"CVE-SCOPED-CHANGED"}) {
		t.Fatalf("scoped echo = %v, want the stamp", scoped.ScopedCVEIDs)
	}

	fullRows := diffLiveRows(t, ctx, db, "diff-full", "g1")
	scopedRows := diffLiveRows(t, ctx, db, "diff-scoped", "g1")
	stamped := func(rows map[string]string) []string {
		var out []string
		for key, payload := range rows {
			if strings.HasPrefix(key, "CVE-SCOPED-CHANGED|") {
				out = append(out, payload)
			}
		}
		return out
	}
	if fullStamped, scopedStamped := stamped(fullRows), stamped(scopedRows); !reflect.DeepEqual(fullStamped, scopedStamped) {
		t.Fatalf("stamped rows differ:\nfull=%v\nscoped=%v", fullStamped, scopedStamped)
	}
	if n := diffLiveRowCount(t, ctx, db, "diff-full", "g1", "CVE-OUTSIDE"); n != 1 {
		t.Fatalf("full pass outside rows = %d, want 1 (stale retracted)", n)
	}
	if n := diffLiveRowCount(t, ctx, db, "diff-scoped", "g1", "CVE-OUTSIDE"); n != 2 {
		t.Fatalf("scoped pass outside rows = %d, want 2 (baseline plus untouched stale)", n)
	}
	if n := diffLiveRowCount(t, ctx, db, "diff-scoped", "g1", "CVE-SCOPED-CHANGED"); n != 1 {
		t.Fatalf("scoped pass stamped rows = %d, want 1 (stale retracted, rewrite kept)", n)
	}
}
