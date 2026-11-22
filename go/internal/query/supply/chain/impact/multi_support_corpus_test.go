// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// The multi-support corpus (#10880 PR-B0). One canonical finding can be
// written from several scopes, one fact per scope and generation. The winners
// read model keeps one row per canonical key, picked across every scope before
// any filter or grant applies; the legacy fact read applies filters and grants
// to the candidate supports first and picks afterwards. This corpus is the
// smallest one where the two orders can disagree, so the single-support seeds
// the earlier parity proofs use cannot show it.
const (
	multiSupportScopeA       = "git-repository-scope:repository:sa"
	multiSupportScopeB       = "image-scope:sb"
	multiSupportDerivedScope = "image://derived/scan-x"
)

// seedMultiSupportCorpus inserts fourteen static finding facts over eight canonical
// keys and two derived-class facts; callers rebuild the winners. Keys and winners:
//
//	m1: SA p=40 cvss 5.0, SB p=80 cvss 9.5 -> SB wins (higher priority)
//	m2: SA p=90 cvss 9.5, SB p=30 cvss 4.0 -> SA wins (higher priority)
//	m3: SA p=60, SB p=60 (fact:m3:a < fact:m3:b)  -> SA wins (fact id)
//	m4: SA p=60 (fact:m4:z), SB p=60 (fact:m4:b)  -> SB wins (fact id)
//	s1: SA only, p=70; s2: SB only, p=65 (single-support controls)
//	p1: SA p=50 precise, SB p=40 comprehensive -> SA wins (precise)
//	p2: SA p=30 precise, SB p=45 comprehensive -> SB wins (comprehensive)
//
// p1 and p2 are the profile divergence: supports of one canonical key can differ
// in precise eligibility because the match reason is not in the key (the OS path
// gives dpkg_exact_affected_version, precise, from a vendor list and
// dpkg_affected_range, comprehensive, from a tagged range for one observed
// version). p2's winner is the comprehensive support, so a profile=precise filter
// answered from the winner hides a finding that has a precise support.
//
// Two derived-class facts at p=99 carry an active derived classification: one
// shares m2's canonical key inside an image://derived scope (excluded by scope
// prefix and by classification), the other is key m5 in ordinary scope SA
// (excluded by classification alone). Neither read admits either: they must
// not win m2, must not appear as m5, and must not appear under any grant.
func seedMultiSupportCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	groupedLiveScope(t, ctx, database, multiSupportScopeA, "gen:a")
	groupedLiveScope(t, ctx, database, multiSupportScopeB, "gen:b")
	groupedLiveScope(t, ctx, database, multiSupportDerivedScope, "gen:derived")
	seed := func(factID, scope, generation, id string, score int, cvss float64) {
		payload := groupedLiveFindingPayload(id, id, score, cvss)
		payload["priority_bucket"] = "low"
		if score >= 70 {
			payload["priority_bucket"] = "critical"
		}
		groupedLiveFact(t, ctx, database, factID, scope, generation, FindingFactKind, payload)
	}
	seed("fact:m1:a", multiSupportScopeA, "gen:a", "m1", 40, 5.0)
	seed("fact:m1:b", multiSupportScopeB, "gen:b", "m1", 80, 9.5)
	seed("fact:m2:a", multiSupportScopeA, "gen:a", "m2", 90, 9.5)
	seed("fact:m2:b", multiSupportScopeB, "gen:b", "m2", 30, 4.0)
	seed("fact:m3:a", multiSupportScopeA, "gen:a", "m3", 60, 6.0)
	seed("fact:m3:b", multiSupportScopeB, "gen:b", "m3", 60, 6.0)
	seed("fact:m4:z", multiSupportScopeA, "gen:a", "m4", 60, 6.0)
	seed("fact:m4:b", multiSupportScopeB, "gen:b", "m4", 60, 6.0)
	seed("fact:s1:a", multiSupportScopeA, "gen:a", "s1", 70, 7.0)
	seed("fact:s2:b", multiSupportScopeB, "gen:b", "s2", 65, 6.5)
	seedComprehensive := func(factID, scope, generation, id string, score int) {
		payload := groupedLiveFindingPayload(id, id, score, 5.0)
		payload["priority_bucket"] = "low"
		payload["detection_profile"] = ProfileComprehensive
		payload["match_reason"] = "dpkg_affected_range"
		groupedLiveFact(t, ctx, database, factID, scope, generation, FindingFactKind, payload)
	}
	seed("fact:p1:a", multiSupportScopeA, "gen:a", "p1", 50, 5.0)
	seedComprehensive("fact:p1:b", multiSupportScopeB, "gen:b", "p1", 40)
	seed("fact:p2:a", multiSupportScopeA, "gen:a", "p2", 30, 5.0)
	seedComprehensive("fact:p2:b", multiSupportScopeB, "gen:b", "p2", 45)
	seed("fact:m2:derived", multiSupportDerivedScope, "gen:derived", "m2", 99, 9.9)
	// A derived classification on a fact in an ordinary scope: the scope-prefix
	// exclusion does not apply, so only the classification keeps it out.
	seed("fact:m5:derived", multiSupportScopeA, "gen:a", "m5", 99, 9.9)
	if _, err := database.ExecContext(ctx, `
INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle, scan_scope_id)
VALUES ('fact:m2:derived', 'derived', 'active', $1),
       ('fact:m5:derived', 'derived', 'active', $1)`, multiSupportDerivedScope); err != nil {
		t.Fatalf("classify derived findings: %v", err)
	}
}

// multiSupportWinnersListArgs mirrors PostgresFindingStore.ListSupplyChainImpactFindings
// so the winners list query can run with a scope grant. The shipped store
// routes granted calls to the legacy read, which is the point of this proof.
func multiSupportWinnersListArgs(ctx context.Context, filter FindingFilter) []any {
	args := []any{
		FindingFactKind, filter.CVEID, filter.PackageID, filter.RepositoryID, filter.SubjectDigest,
		filter.Status, filter.AdvisoryID, filter.Ecosystem, filter.ServiceID, filter.WorkloadID,
		filter.Environment, filter.Severity, filter.DetectionProfile, filter.PriorityBucket,
		filter.MinPriorityScore, filter.ImageRef, filter.AfterFindingID, NormalizeSort(filter.Sort),
		filter.Limit, filter.SuppressionState, filter.IncludeSuppressed,
		array.Of(filter.AllowedRepositoryIDs), array.Of(filter.AllowedScopeIDs), SuppressionReadAt(nil),
	}
	return append(args, supplyChainImpactCallerAuthorizationArgs(ctx)...)
}

func multiSupportWinnersList(ctx context.Context, database *sql.DB, filter FindingFilter) ([]FindingRow, error) {
	rows, err := database.QueryContext(ctx, ListFindingsFromWinnersQuery, multiSupportWinnersListArgs(ctx, filter)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FindingRow
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

func multiSupportAggregateArgs(ctx context.Context, f AggregateFilter, at time.Time) []any {
	args := []any{
		f.CVEID, f.PackageID, f.RepositoryID, f.SubjectDigest, f.Status, f.AdvisoryID, f.Ecosystem,
		f.ServiceID, f.WorkloadID, f.Environment, f.Severity, f.DetectionProfile, f.PriorityBucket,
		f.MinPriorityScore, f.SuppressionState, f.IncludeSuppressed, f.ImageRef,
		array.Of(f.AllowedRepositoryIDs), array.Of(f.AllowedScopeIDs), at,
	}
	return append(args, supplyChainImpactCallerAuthorizationArgs(ctx)...)
}

func multiSupportWinnersCount(
	ctx context.Context, database *sql.DB, f AggregateFilter, at time.Time,
) (AggregateCount, error) {
	row := database.QueryRowContext(ctx, AggregateWinnersCombinedCountQuery, multiSupportAggregateArgs(ctx, f, at)...)
	var total, affected, exact, derived, possibly, notAffected sql.NullInt64
	var priority, severity []byte
	if err := row.Scan(&total, &affected, &exact, &derived, &possibly, &notAffected, &priority, &severity); err != nil {
		return AggregateCount{}, err
	}
	count := AggregateCount{
		TotalFindings: int(total.Int64), AffectedFindings: int(affected.Int64), AffectedExact: int(exact.Int64),
		AffectedDerived: int(derived.Int64), PossiblyAffected: int(possibly.Int64), NotAffected: int(notAffected.Int64),
		ByPriorityBucket: map[string]int{}, BySeverity: map[string]int{},
	}
	if err := decodeAggregateBucketJSON(priority, count.ByPriorityBucket); err != nil {
		return AggregateCount{}, err
	}
	if err := decodeAggregateBucketJSON(severity, count.BySeverity); err != nil {
		return AggregateCount{}, err
	}
	return count, nil
}

func multiSupportWinnersInventory(
	ctx context.Context, database *sql.DB, f AggregateFilter, dimension InventoryDimension, at time.Time,
) ([]InventoryRow, error) {
	expr, err := supplyChainImpactInventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	args := append(multiSupportAggregateArgs(ctx, f, at), 50, 0)
	rows, err := database.QueryContext(ctx, AggregateWinnersInventoryQuery(expr), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []InventoryRow
	for rows.Next() {
		var bucket string
		var n int64
		if err := rows.Scan(&bucket, &n); err != nil {
			return nil, err
		}
		out = append(out, InventoryRow{Dimension: dimension, Value: strings.TrimSpace(bucket), Count: int(n)})
	}
	return out, rows.Err()
}

// multiSupportDescribe renders a page as sorted "id(p=priority)" tokens so an
// expectation shows both the finding set and which support's payload served it.
func multiSupportDescribe(rows []FindingRow) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf("%s(p=%d)", strings.TrimPrefix(row.FindingID, "finding:"), row.PriorityScore))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}
