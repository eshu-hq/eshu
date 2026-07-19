// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubGraphQuery records the Cypher + params sent to the graph and returns
// canned rows per query. This is the unit-test substitute for Neo4jReader,
// split out from package_registry_aggregates_test.go (which exercises the
// HTTP handler via stubPackageRegistryAggregateStore) to stay under the
// repository's 500-line file cap. These tests exercise
// GraphPackageRegistryAggregateStore, the production Reader, directly.
type stubGraphQuery struct {
	responses map[string][]map[string]any
	calls     []struct {
		Cypher string
		Params map[string]any
	}
	err error
}

func (s *stubGraphQuery) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s.calls = append(s.calls, struct {
		Cypher string
		Params map[string]any
	}{Cypher: cypher, Params: params})
	if s.err != nil {
		return nil, s.err
	}
	for k, rows := range s.responses {
		if strings.Contains(cypher, k) {
			return rows, nil
		}
	}
	return nil, nil
}

func (s *stubGraphQuery) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, errors.New("RunSingle not used by package-registry aggregates")
}

// TestGraphPackageRegistryAggregateStoreCountShapeIsHotPathEligible asserts on
// the actual Cypher emitted by the production Reader. The fixture exercise
// proves the WHERE clause uses parameter-bound predicates against indexed
// properties (cookbook Area-5 / PatternOutgoingCountAgg shape). A full
// PROFILE proof against the pinned NornicDB binary is the operator gate
// (see PR description) — this test is the in-process shape guard so a future
// refactor can't silently drop the hot-path-friendly anchor predicates.
func TestGraphPackageRegistryAggregateStoreCountShapeIsHotPathEligible(t *testing.T) {
	t.Parallel()

	graph := &stubGraphQuery{
		responses: map[string][]map[string]any{
			// The rollup query uses a CASE expression on `p.ecosystem` so
			// empty-string properties surface as `unknown` alongside NULLs.
			"count(p) AS total":             {{"total": int64(7)}},
			"WHEN p.ecosystem IS NULL OR p": {{"bucket": "npm", "bucket_count": int64(5)}, {"bucket": "pypi", "bucket_count": int64(2)}},
		},
	}
	store := NewGraphPackageRegistryAggregateStore(graph)
	count, err := store.CountPackageRegistryPackages(context.Background(), PackageRegistryAggregateFilter{Ecosystem: "npm"})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count.TotalPackages != 7 {
		t.Fatalf("TotalPackages = %d, want 7", count.TotalPackages)
	}
	if count.ByEcosystem["npm"] != 5 {
		t.Fatalf("ByEcosystem[npm] = %d, want 5", count.ByEcosystem["npm"])
	}

	if len(graph.calls) < 1 {
		t.Fatal("graph.Run never called")
	}
	first := graph.calls[0]
	if !strings.Contains(first.Cypher, "MATCH (p:Package)") {
		t.Fatalf("count Cypher missing `MATCH (p:Package)` label-property anchor: %s", first.Cypher)
	}
	if !strings.Contains(first.Cypher, "p.ecosystem = $ecosystem") {
		t.Fatalf("count Cypher missing indexed `p.ecosystem` anchor predicate (hot-path requirement): %s", first.Cypher)
	}
	if got, want := first.Params["ecosystem"], "npm"; got != want {
		t.Fatalf("ecosystem param = %v, want %q", got, want)
	}
}

func TestGraphPackageRegistryAggregateStoreInventoryShapeIsHotPathEligible(t *testing.T) {
	t.Parallel()

	graph := &stubGraphQuery{
		responses: map[string][]map[string]any{
			"SKIP $offset": {{"bucket": "registry.example", "bucket_count": int64(3)}},
		},
	}
	store := NewGraphPackageRegistryAggregateStore(graph)
	rows, err := store.PackageRegistryPackageInventory(
		context.Background(),
		PackageRegistryAggregateFilter{Ecosystem: "npm"},
		PackageRegistryInventoryByRegistry,
		11,
		0,
	)
	if err != nil {
		t.Fatalf("PackageRegistryPackageInventory: %v", err)
	}
	if len(rows) != 1 || rows[0].Value != "registry.example" || rows[0].Count != 3 {
		t.Fatalf("rows = %+v, want one bucket {registry.example, 3}", rows)
	}

	if len(graph.calls) != 1 {
		t.Fatalf("graph.Run called %d times, want 1", len(graph.calls))
	}
	cypher := graph.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (p:Package)") {
		t.Fatalf("inventory Cypher missing label-property anchor: %s", cypher)
	}
	if !strings.Contains(cypher, "p.registry") {
		t.Fatalf("inventory Cypher did not group by the requested dimension `p.registry`: %s", cypher)
	}
	if !strings.Contains(cypher, "ORDER BY bucket_count DESC") {
		t.Fatalf("inventory Cypher missing deterministic ordering: %s", cypher)
	}
	if got, want := graph.calls[0].Params["limit"], 11; got != want {
		t.Fatalf("limit param = %v, want %v", got, want)
	}
}

func TestGraphPackageRegistryAggregateStoreNormalizesEmptyStringBuckets(t *testing.T) {
	t.Parallel()

	// Package-registry facts commonly leave optional fields like `namespace`
	// as the empty string for ecosystems without a namespace concept (npm
	// unscoped packages, pypi, etc). A plain `coalesce(p.namespace,
	// 'unknown')` would only collapse NULLs and emit `""` as a bucket; the
	// CASE expression must instead map both NULL and empty-string to
	// `unknown` so callers never see an empty bucket key.
	for _, kind := range []struct {
		name          string
		dimension     PackageRegistryInventoryDimension
		propertyMatch string
	}{
		{"namespace", PackageRegistryInventoryByNamespace, "p.namespace"},
		{"ecosystem", PackageRegistryInventoryByEcosystem, "p.ecosystem"},
		{"registry", PackageRegistryInventoryByRegistry, "p.registry"},
		{"package_manager", PackageRegistryInventoryByPackageManager, "p.package_manager"},
		{"visibility", PackageRegistryInventoryByVisibility, "p.visibility"},
	} {
		kind := kind
		t.Run(kind.name, func(t *testing.T) {
			t.Parallel()
			graph := &stubGraphQuery{}
			store := NewGraphPackageRegistryAggregateStore(graph)
			_, err := store.PackageRegistryPackageInventory(
				context.Background(),
				PackageRegistryAggregateFilter{},
				kind.dimension,
				11,
				0,
			)
			if err != nil {
				t.Fatalf("inventory(%s): %v", kind.name, err)
			}
			if len(graph.calls) != 1 {
				t.Fatalf("graph.Run called %d times, want 1", len(graph.calls))
			}
			cypher := graph.calls[0].Cypher
			// Both the NULL branch and the empty-string branch must reference
			// the same dimension property so the substituted bucket key is
			// `unknown` whenever the field is absent or blank.
			nullClause := "WHEN " + kind.propertyMatch + " IS NULL"
			emptyClause := "OR " + kind.propertyMatch + " = ''"
			elseClause := "ELSE " + kind.propertyMatch
			if !strings.Contains(cypher, nullClause) {
				t.Fatalf("missing NULL branch for %s: %s", kind.propertyMatch, cypher)
			}
			if !strings.Contains(cypher, emptyClause) {
				t.Fatalf("missing empty-string branch for %s (plain coalesce would emit \"\" as a bucket key): %s", kind.propertyMatch, cypher)
			}
			if !strings.Contains(cypher, elseClause) {
				t.Fatalf("missing ELSE branch for %s: %s", kind.propertyMatch, cypher)
			}
			if !strings.Contains(cypher, "THEN 'unknown'") {
				t.Fatalf("missing THEN 'unknown' for %s: %s", kind.propertyMatch, cypher)
			}
		})
	}
}

func TestGraphPackageRegistryAggregateStoreRejectsUnsafeDimension(t *testing.T) {
	t.Parallel()

	graph := &stubGraphQuery{}
	store := NewGraphPackageRegistryAggregateStore(graph)
	_, err := store.PackageRegistryPackageInventory(
		context.Background(),
		PackageRegistryAggregateFilter{},
		PackageRegistryInventoryDimension("normalized_name"),
		100,
		0,
	)
	if err == nil {
		t.Fatal("expected error for unknown dimension; got nil")
	}
	if len(graph.calls) != 0 {
		t.Fatal("graph queried for unknown dimension; substitution must be guarded before any graph call")
	}
}
