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
	for _, want := range []string{
		"domain = ANY($2::text[])",
		"same.domain = ANY($2::text[])",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing domain allowlist predicate %q:\n%s", want, query)
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
