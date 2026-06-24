// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

// fakeChainCorrelationStore returns scripted correlation rows per filter so the
// resolver's two-phase (consumption then batched publication/ownership) read can
// be exercised without Postgres.
type fakeChainCorrelationStore struct {
	consumption []PackageRegistryCorrelationRow
	publication []PackageRegistryCorrelationRow
	calls       []PackageRegistryCorrelationFilter
}

func (s *fakeChainCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter PackageRegistryCorrelationFilter,
) ([]PackageRegistryCorrelationRow, error) {
	s.calls = append(s.calls, filter)
	if filter.RepositoryID != "" {
		// Phase-1: consumption read anchored on consumer repo.
		out := make([]PackageRegistryCorrelationRow, 0, len(s.consumption))
		for _, row := range s.consumption {
			if filter.AfterCorrelationID != "" && row.CorrelationID <= filter.AfterCorrelationID {
				continue
			}
			out = append(out, row)
			if filter.Limit > 0 && len(out) >= filter.Limit {
				break
			}
		}
		return out, nil
	}
	// Phase-2: batched publisher read keyed by PackageIDs + RelationshipKinds.
	out := make([]PackageRegistryCorrelationRow, 0, len(s.publication))
	for _, row := range s.publication {
		if len(filter.PackageIDs) > 0 && !stringSliceContains(filter.PackageIDs, row.PackageID) {
			continue
		}
		if len(filter.RelationshipKinds) > 0 && !stringSliceContains(filter.RelationshipKinds, row.RelationshipKind) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func TestResolvePackageDependencyChainsJoinsConsumerToPublisher(t *testing.T) {
	t.Parallel()

	store := &fakeChainCorrelationStore{
		consumption: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "consume-1",
				RelationshipKind: "consumption",
				PackageID:        "pkg:npm://registry.example/team-api",
				PackageName:      "@acme/team-api",
				Ecosystem:        "npm",
				RepositoryID:     "repo-consumer",
				RepositoryName:   "consumer-app",
				DependencyRange:  "^1.2.0",
				Outcome:          "exact",
				ProvenanceOnly:   false,
				CanonicalWrites:  1,
			},
		},
		publication: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "publish-1",
				RelationshipKind: "publication",
				PackageID:        "pkg:npm://registry.example/team-api",
				PackageName:      "@acme/team-api",
				Ecosystem:        "npm",
				RepositoryID:     "repo-publisher",
				RepositoryName:   "team-api",
				Outcome:          "exact",
				ProvenanceOnly:   true,
				CanonicalWrites:  0,
			},
		},
	}

	chains, err := ResolvePackageDependencyChains(
		context.Background(),
		store,
		PackageDependencyChainRequest{RepositoryID: "repo-consumer", Limit: 50},
	)
	if err != nil {
		t.Fatalf("ResolvePackageDependencyChains: %v", err)
	}

	// Two-phase read: one repo-anchored consumption read, one batched
	// publication/ownership read keyed by the distinct package set.
	if got, want := len(store.calls), 2; got != want {
		t.Fatalf("store calls = %d, want %d (one consumption + one batched publisher read)", got, want)
	}
	if got := store.calls[0].RepositoryID; got != "repo-consumer" {
		t.Fatalf("phase-1 RepositoryID = %q, want repo-consumer", got)
	}
	if got := store.calls[0].RelationshipKind; got != "consumption" {
		t.Fatalf("phase-1 must anchor consumption, got RelationshipKind=%q", got)
	}
	if got, want := len(store.calls[1].PackageIDs), 1; got != want {
		t.Fatalf("phase-2 PackageIDs = %d, want %d (batched, not 1+N)", got, want)
	}
	if got := store.calls[1].RepositoryID; got != "" {
		t.Fatalf("phase-2 must not anchor on the consumer repo, got RepositoryID=%q", got)
	}

	if got, want := len(chains), 1; got != want {
		t.Fatalf("len(chains) = %d, want %d", got, want)
	}
	chain := chains[0]
	if chain.ConsumerRepositoryID != "repo-consumer" {
		t.Fatalf("ConsumerRepositoryID = %q, want repo-consumer", chain.ConsumerRepositoryID)
	}
	if chain.PackageID != "pkg:npm://registry.example/team-api" {
		t.Fatalf("PackageID = %q, want the consumed package", chain.PackageID)
	}
	// Consumption leg is canonical truth.
	if chain.ConsumptionProvenanceOnly {
		t.Fatal("consumption leg must not be provenance-only (canonical, canonical_writes=1)")
	}
	if got, want := len(chain.Publishers), 1; got != want {
		t.Fatalf("len(Publishers) = %d, want %d", got, want)
	}
	pub := chain.Publishers[0]
	if pub.RepositoryID != "repo-publisher" {
		t.Fatalf("publisher RepositoryID = %q, want repo-publisher", pub.RepositoryID)
	}
	// Publisher leg is provenance-only -> must be labeled inferred, never exact.
	if !pub.ProvenanceOnly {
		t.Fatal("publisher leg must carry provenance_only=true so it renders as inferred, not exact")
	}
	if chain.Ambiguous {
		t.Fatal("single publisher must not be marked ambiguous")
	}
}

func TestResolvePackageDependencyChainsKeepsNoPublisherTerminal(t *testing.T) {
	t.Parallel()

	store := &fakeChainCorrelationStore{
		consumption: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "consume-1",
				RelationshipKind: "consumption",
				PackageID:        "pkg:npm://registry.example/no-publisher",
				RepositoryID:     "repo-consumer",
				CanonicalWrites:  1,
			},
		},
		publication: nil, // no publisher correlation exists for the package
	}

	chains, err := ResolvePackageDependencyChains(
		context.Background(),
		store,
		PackageDependencyChainRequest{RepositoryID: "repo-consumer", Limit: 50},
	)
	if err != nil {
		t.Fatalf("ResolvePackageDependencyChains: %v", err)
	}
	if got, want := len(chains), 1; got != want {
		t.Fatalf("len(chains) = %d, want %d (consumption row must not be dropped)", got, want)
	}
	if len(chains[0].Publishers) != 0 {
		t.Fatalf("no-publisher chain must terminate at the package, got %d publishers", len(chains[0].Publishers))
	}
	if chains[0].Ambiguous {
		t.Fatal("a terminal (no-publisher) chain is not ambiguous")
	}
}

func TestResolvePackageDependencyChainsMarksMultiplePublishersAmbiguous(t *testing.T) {
	t.Parallel()

	store := &fakeChainCorrelationStore{
		consumption: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "consume-1",
				RelationshipKind: "consumption",
				PackageID:        "pkg:npm://registry.example/contested",
				RepositoryID:     "repo-consumer",
				CanonicalWrites:  1,
			},
		},
		publication: []PackageRegistryCorrelationRow{
			{CorrelationID: "publish-a", RelationshipKind: "publication", PackageID: "pkg:npm://registry.example/contested", RepositoryID: "repo-pub-a", ProvenanceOnly: true},
			{CorrelationID: "publish-b", RelationshipKind: "ownership", PackageID: "pkg:npm://registry.example/contested", RepositoryID: "repo-pub-b", ProvenanceOnly: true},
		},
	}

	chains, err := ResolvePackageDependencyChains(
		context.Background(),
		store,
		PackageDependencyChainRequest{RepositoryID: "repo-consumer", Limit: 50},
	)
	if err != nil {
		t.Fatalf("ResolvePackageDependencyChains: %v", err)
	}
	if got, want := len(chains), 1; got != want {
		t.Fatalf("len(chains) = %d, want %d", got, want)
	}
	if got, want := len(chains[0].Publishers), 2; got != want {
		t.Fatalf("len(Publishers) = %d, want %d (never collapse to a single asserted publisher)", got, want)
	}
	if !chains[0].Ambiguous {
		t.Fatal("multiple candidate publishers must mark the chain ambiguous")
	}
}

// TestResolvePackageDependencyChainsPhase2FiltersPublisherKinds verifies that
// the phase-2 batched read uses RelationshipKinds to restrict the SQL query to
// publisher kinds (publication/ownership) before the LIMIT page. This prevents
// a popular consumed package with many consumer rows from filling the bounded
// page and silently dropping publisher rows.
func TestResolvePackageDependencyChainsPhase2FiltersPublisherKinds(t *testing.T) {
	t.Parallel()

	store := &fakeChainCorrelationStore{
		consumption: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "consume-1",
				RelationshipKind: "consumption",
				PackageID:        "pkg:npm://registry.example/popular",
				PackageName:      "@acme/popular",
				Ecosystem:        "npm",
				RepositoryID:     "repo-consumer",
				CanonicalWrites:  1,
			},
		},
		publication: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "publish-1",
				RelationshipKind: "publication",
				PackageID:        "pkg:npm://registry.example/popular",
				RepositoryID:     "repo-publisher",
				ProvenanceOnly:   true,
				CanonicalWrites:  0,
			},
		},
	}

	chains, err := ResolvePackageDependencyChains(
		context.Background(),
		store,
		PackageDependencyChainRequest{RepositoryID: "repo-consumer", Limit: 50},
	)
	if err != nil {
		t.Fatalf("ResolvePackageDependencyChains: %v", err)
	}

	// Phase-2 filter must use RelationshipKinds so the SQL WHERE restricts rows
	// to publisher kinds before LIMIT — consumption rows must never appear in the
	// phase-2 result set regardless of how many consumer rows exist for the package.
	if got, want := len(store.calls), 2; got != want {
		t.Fatalf("store calls = %d, want %d", got, want)
	}
	phase2 := store.calls[1]
	if len(phase2.RelationshipKinds) == 0 {
		t.Fatal("phase-2 filter must carry RelationshipKinds so the SQL WHERE restricts to publisher kinds before LIMIT")
	}
	for _, kind := range phase2.RelationshipKinds {
		if kind == packageConsumptionRelationshipKind {
			t.Fatalf("phase-2 RelationshipKinds must not include %q (consumption must be excluded before LIMIT)", kind)
		}
	}

	if got, want := len(chains), 1; got != want {
		t.Fatalf("len(chains) = %d, want %d", got, want)
	}
	if got, want := len(chains[0].Publishers), 1; got != want {
		t.Fatalf("publisher must not be dropped; len(Publishers) = %d, want %d", got, want)
	}
}

func TestResolvePackageDependencyChainsDropsSelfPublisher(t *testing.T) {
	t.Parallel()

	store := &fakeChainCorrelationStore{
		consumption: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "consume-1",
				RelationshipKind: "consumption",
				PackageID:        "pkg:npm://registry.example/self",
				RepositoryID:     "repo-consumer",
				CanonicalWrites:  1,
			},
		},
		publication: []PackageRegistryCorrelationRow{
			{CorrelationID: "publish-self", RelationshipKind: "publication", PackageID: "pkg:npm://registry.example/self", RepositoryID: "repo-consumer", ProvenanceOnly: true},
		},
	}

	chains, err := ResolvePackageDependencyChains(
		context.Background(),
		store,
		PackageDependencyChainRequest{RepositoryID: "repo-consumer", Limit: 50},
	)
	if err != nil {
		t.Fatalf("ResolvePackageDependencyChains: %v", err)
	}
	if got, want := len(chains), 1; got != want {
		t.Fatalf("len(chains) = %d, want %d", got, want)
	}
	if len(chains[0].Publishers) != 0 {
		t.Fatalf("publisher == consumer must not surface as a repo-to-repo link, got %d publishers", len(chains[0].Publishers))
	}
}
