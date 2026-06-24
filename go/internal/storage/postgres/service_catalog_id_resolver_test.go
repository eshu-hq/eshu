// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestServiceCatalogIDResolverResolvesUniqueCatalogService(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"component:default/checkout"}}},
		},
	}
	resolver := NewServiceCatalogIDResolver(db)

	got, err := resolver.ResolveCatalogServiceID(context.Background(), "workload:checkout")
	if err != nil {
		t.Fatalf("ResolveCatalogServiceID() error = %v, want nil", err)
	}
	if got != "component:default/checkout" {
		t.Fatalf("catalog service id = %q, want component:default/checkout", got)
	}
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want one bounded resolve", len(db.queries))
	}
	if got := db.queries[0].args[0]; got != "workload:checkout" {
		t.Fatalf("resolve arg = %v, want the workload id", got)
	}
}

func TestServiceCatalogIDResolverBlankWorkloadIsNoOp(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	resolver := NewServiceCatalogIDResolver(db)

	got, err := resolver.ResolveCatalogServiceID(context.Background(), "   ")
	if err != nil {
		t.Fatalf("ResolveCatalogServiceID() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("catalog service id = %q, want empty for blank workload", got)
	}
	if len(db.queries) != 0 {
		t.Fatalf("queries = %d, want no query for blank workload", len(db.queries))
	}
}

func TestServiceCatalogIDResolverUnresolvedIsEmptyNotError(t *testing.T) {
	t.Parallel()

	// No admissible correlation row: the resolver returns an empty id and a nil
	// error so the caller leaves the section unsupported rather than fabricating
	// a false "no incidents" attribution.
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	resolver := NewServiceCatalogIDResolver(db)

	got, err := resolver.ResolveCatalogServiceID(context.Background(), "workload:orphan")
	if err != nil {
		t.Fatalf("ResolveCatalogServiceID() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("catalog service id = %q, want empty for unresolved workload", got)
	}
}

func TestServiceCatalogIDResolverFailsClosedOnAmbiguity(t *testing.T) {
	t.Parallel()

	// A workload that maps to more than one active catalog service is ambiguous;
	// the resolver must fail closed so the caller never attributes service-scoped
	// evidence (incidents) to the wrong catalog service.
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"component:default/checkout"},
				{"component:default/checkout-legacy"},
			}},
		},
	}
	resolver := NewServiceCatalogIDResolver(db)

	got, err := resolver.ResolveCatalogServiceID(context.Background(), "workload:checkout")
	if !errors.Is(err, ErrAmbiguousCatalogService) {
		t.Fatalf("error = %v, want ErrAmbiguousCatalogService", err)
	}
	if got != "" {
		t.Fatalf("catalog service id = %q, want empty on ambiguity", got)
	}
}

func TestServiceCatalogIDResolverDuplicateRowsForSameIDIsNotAmbiguous(t *testing.T) {
	t.Parallel()

	// Repeated rows for the same catalog service id collapse to one resolution;
	// only more than one DISTINCT catalog service is ambiguous.
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"component:default/checkout"},
				{"component:default/checkout"},
			}},
		},
	}
	resolver := NewServiceCatalogIDResolver(db)

	got, err := resolver.ResolveCatalogServiceID(context.Background(), "workload:checkout")
	if err != nil {
		t.Fatalf("ResolveCatalogServiceID() error = %v, want nil for same-id duplicate rows", err)
	}
	if got != "component:default/checkout" {
		t.Fatalf("catalog service id = %q, want component:default/checkout", got)
	}
}

func TestServiceCatalogIDResolverQueryErrorPropagates(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{err: errors.New("connection reset")}}}
	resolver := NewServiceCatalogIDResolver(db)

	if _, err := resolver.ResolveCatalogServiceID(context.Background(), "workload:checkout"); err == nil {
		t.Fatal("ResolveCatalogServiceID() must propagate the QueryContext error")
	}
}

func TestServiceCatalogIDResolverNilQueryerErrors(t *testing.T) {
	t.Parallel()

	resolver := NewServiceCatalogIDResolver(nil)
	if _, err := resolver.ResolveCatalogServiceID(context.Background(), "workload:checkout"); err == nil {
		t.Fatal("ResolveCatalogServiceID() with nil queryer must error")
	}
}

func TestServiceCatalogIDResolverQueryUsesDurableExactFailClosedGate(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"reducer_service_catalog_correlation",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"payload->>'provenance_only' = 'false'",
		"payload->>'outcome' IN ('exact', 'derived')",
		"fact.payload->>'workload_id' = $1",
		"NULLIF(fact.payload->>'service_id', '') IS NOT NULL",
	} {
		if !strings.Contains(serviceCatalogIDForWorkloadQuery, want) {
			t.Errorf("resolver query missing durable gate %q", want)
		}
	}
}
