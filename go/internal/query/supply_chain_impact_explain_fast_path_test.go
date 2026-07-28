// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

func TestExplainSupplyChainImpactUsesPublicFindingIDFastPath(t *testing.T) {
	queryer := &sequentialSupplyChainExplanationQueryer{
		t: t,
		responses: [][][]driver.Value{{
			supplyChainExplanationTestRow("finding:public"),
		}},
	}
	store := NewPostgresSupplyChainImpactFindingStore(queryer)

	explanation, err := store.ExplainSupplyChainImpact(
		context.Background(),
		SupplyChainImpactExplanationFilter{FindingID: "finding:public"},
	)
	if err != nil {
		t.Fatalf("ExplainSupplyChainImpact() error = %v, want nil", err)
	}
	if got, want := explanation.Finding.FindingID, "finding:public"; got != want {
		t.Fatalf("FindingID = %q, want %q", got, want)
	}
	if got, want := queryer.queries, []string{explainSupplyChainImpactFindingByPublicIDQuery}; !equalExplanationQueries(got, want) {
		t.Fatalf("queries = %#v, want public-ID fast path only", got)
	}
}

func TestExplainSupplyChainImpactFallsBackForLegacyFindingIdentity(t *testing.T) {
	queryer := &sequentialSupplyChainExplanationQueryer{
		t: t,
		responses: [][][]driver.Value{
			nil,
			{supplyChainExplanationTestRow("finding:legacy")},
		},
	}
	store := NewPostgresSupplyChainImpactFindingStore(queryer)

	explanation, err := store.ExplainSupplyChainImpact(
		context.Background(),
		SupplyChainImpactExplanationFilter{FindingID: "fact:legacy"},
	)
	if err != nil {
		t.Fatalf("ExplainSupplyChainImpact() error = %v, want nil", err)
	}
	if got, want := explanation.Finding.FindingID, "finding:legacy"; got != want {
		t.Fatalf("FindingID = %q, want %q", got, want)
	}
	if got, want := queryer.queries, []string{
		explainSupplyChainImpactFindingByPublicIDQuery,
		explainSupplyChainImpactFindingQuery,
	}; !equalExplanationQueries(got, want) {
		t.Fatalf("queries = %#v, want fast-path miss followed by compatibility query", got)
	}
}

func TestExplainSupplyChainImpactFastPathPreservesAmbiguity(t *testing.T) {
	queryer := &sequentialSupplyChainExplanationQueryer{
		t: t,
		responses: [][][]driver.Value{{
			supplyChainExplanationTestRow("finding:duplicate"),
			supplyChainExplanationTestRow("finding:duplicate"),
		}},
	}
	store := NewPostgresSupplyChainImpactFindingStore(queryer)

	_, err := store.ExplainSupplyChainImpact(
		context.Background(),
		SupplyChainImpactExplanationFilter{FindingID: "finding:duplicate"},
	)
	if !errors.Is(err, ErrSupplyChainImpactExplanationAmbiguous) {
		t.Fatalf("ExplainSupplyChainImpact() error = %v, want ambiguity", err)
	}
	if got := len(queryer.queries); got != 1 {
		t.Fatalf("query count = %d, want 1 without compatibility fallback", got)
	}
}

func TestExplainSupplyChainImpactNonFindingScopeUsesCompatibilityQuery(t *testing.T) {
	queryer := &sequentialSupplyChainExplanationQueryer{
		t: t,
		responses: [][][]driver.Value{{
			supplyChainExplanationTestRow("finding:bounded"),
		}},
	}
	store := NewPostgresSupplyChainImpactFindingStore(queryer)

	_, err := store.ExplainSupplyChainImpact(
		context.Background(),
		SupplyChainImpactExplanationFilter{
			CVEID:     "CVE-2026-54654",
			PackageID: "pkg:deb/example/bounded",
		},
	)
	if err != nil {
		t.Fatalf("ExplainSupplyChainImpact() error = %v, want nil", err)
	}
	if got, want := queryer.queries, []string{explainSupplyChainImpactFindingQuery}; !equalExplanationQueries(got, want) {
		t.Fatalf("queries = %#v, want compatibility query only", got)
	}
}

func TestExplainSupplyChainImpactPublicIDQueryUsesIndexedPredicate(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.payload->>'finding_id' = $2",
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"LIMIT 2",
	} {
		if !strings.Contains(explainSupplyChainImpactFindingByPublicIDQuery, want) {
			t.Fatalf("public-ID explain query missing %q:\n%s", want, explainSupplyChainImpactFindingByPublicIDQuery)
		}
	}
	for _, want := range []string{
		"fact_id = $2",
		"canonical_key = $2",
	} {
		if !strings.Contains(explainSupplyChainImpactFindingQuery, want) {
			t.Fatalf("compatibility explain query missing %q:\n%s", want, explainSupplyChainImpactFindingQuery)
		}
	}
}

type sequentialSupplyChainExplanationQueryer struct {
	t         *testing.T
	responses [][][]driver.Value
	queries   []string
}

func (q *sequentialSupplyChainExplanationQueryer) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (*sql.Rows, error) {
	q.t.Helper()
	index := len(q.queries)
	if index >= len(q.responses) {
		q.t.Fatalf("unexpected query %d:\n%s", index+1, query)
	}
	q.queries = append(q.queries, query)
	db, _ := openScopeQueryerTestDB(
		q.t,
		[]string{"finding_id", "source_confidence", "payload"},
		q.responses[index],
	)
	return db.QueryContext(ctx, query, args...)
}

func supplyChainExplanationTestRow(findingID string) []driver.Value {
	return []driver.Value{
		findingID,
		"high",
		[]byte(`{"finding_id":"` + findingID + `","evidence_fact_ids":[]}`),
	}
}

func equalExplanationQueries(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
