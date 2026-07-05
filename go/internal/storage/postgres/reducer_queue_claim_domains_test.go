// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueClaimCanFilterByMultipleDomains(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "code-graph-lane",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomains: []reducer.Domain{
			reducer.DomainSQLRelationshipMaterialization,
			reducer.DomainInheritanceMaterialization,
		},
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	query := db.queries[0].query
	if !strings.Contains(query, "domain = ANY($2::text[])") {
		t.Fatalf("claim query missing domain allowlist predicate:\n%s", query)
	}
	want := []string{
		string(reducer.DomainSQLRelationshipMaterialization),
		string(reducer.DomainInheritanceMaterialization),
	}
	if got := db.queries[0].args[1]; !reflect.DeepEqual(got, want) {
		t.Fatalf("domain filter arg = %#v, want %#v", got, want)
	}
}

func TestClaimBatchCanFilterByMultipleDomains(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "code-graph-lane",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomains: []reducer.Domain{
			reducer.DomainSQLRelationshipMaterialization,
			reducer.DomainInheritanceMaterialization,
		},
	}

	if _, err := queue.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v, want nil", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "domain = ANY($2::text[])") {
		t.Fatalf("batch claim query missing domain allowlist predicate:\n%s", query)
	}
	// Pre-rank-once-rewrite (#3624 Track 2), the domain allowlist was
	// re-applied at each correlated "same" representative-picker call site
	// ("same.domain = ANY($2::text[])"). The rewrite applies the allowlist
	// exactly once in base's WHERE clause; every downstream representative
	// CTE (reps_ranked, reps) derives from base, and the conflict-key
	// representative is the reps.same_rn = 1 row selected in the candidate CTE,
	// so a domain-filtered row can never become a representative in the first
	// place — re-checking the filter there would be redundant, not a lost
	// guarantee. Confirm the shapes that make the single base-level filter
	// binding for the representative: reps_ranked derives from base's
	// readiness-filtered rows, and the representative is the reps.same_rn = 1
	// row (no separate correlated same-representative subquery).
	for _, want := range []string{
		"reps_ranked AS MATERIALIZED (",
		"FROM base\n    WHERE readiness_ok",
		"WHERE reps.same_rn = 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing rank-once representative %q deriving from the domain-filtered base:\n%s", want, query)
		}
	}
	want := []string{
		string(reducer.DomainSQLRelationshipMaterialization),
		string(reducer.DomainInheritanceMaterialization),
	}
	if got := db.queries[0].args[1]; !reflect.DeepEqual(got, want) {
		t.Fatalf("domain filter arg = %#v, want %#v", got, want)
	}
}
