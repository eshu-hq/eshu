// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"slices"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func TestDependencyValidate(t *testing.T) {
	t.Parallel()

	t.Run("empty producer set is rejected", func(t *testing.T) {
		t.Parallel()
		if err := (reducercontract.CrossScopeDependency{}).Validate(); err == nil {
			t.Fatal("empty cross-scope dependency must be rejected")
		}
	})

	t.Run("unregistered producer domain is rejected", func(t *testing.T) {
		t.Parallel()
		dep := reducercontract.CrossScopeDependency{
			ProducerDomains: []reducercontract.Domain{reducercontract.Domain("not_a_real_domain")},
		}
		if err := dep.Validate(); err == nil {
			t.Fatal("cross-scope dependency naming an unregistered producer must be rejected")
		}
	})

	t.Run("registered producer domain is accepted", func(t *testing.T) {
		t.Parallel()
		dep := reducercontract.CrossScopeDependency{
			ProducerDomains: []reducercontract.Domain{reducercontract.DomainContainerImageIdentity},
		}
		if err := dep.Validate(); err != nil {
			t.Fatalf("valid cross-scope dependency rejected: %v", err)
		}
	})
}

// TestDependencyCatalogIsValid asserts every entry in the single source of
// truth names a registered consumer and only registered producers, so a typo
// in the catalog fails here rather than silently disabling completion replay.
func TestDependencyCatalogIsValid(t *testing.T) {
	t.Parallel()

	catalog := dependencyCatalog()
	if len(catalog) == 0 {
		t.Fatal("cross-scope dependency catalog must not be empty")
	}
	for consumer, dependency := range catalog {
		if err := consumer.Validate(); err != nil {
			t.Errorf("catalog consumer %q is not a registered domain: %v", consumer, err)
		}
		if err := dependency.Validate(); err != nil {
			t.Errorf("catalog entry for consumer %q is invalid: %v", consumer, err)
		}
	}
}

func TestCompletionEdgesExposeCatalogExactly(t *testing.T) {
	t.Parallel()
	want := []CompletionEdge{
		{Producer: reducercontract.DomainCICDRunCorrelation, Consumer: reducercontract.DomainSupplyChainImpact},
		{Producer: reducercontract.DomainContainerImageIdentity, Consumer: reducercontract.DomainCICDRunCorrelation},
		{Producer: reducercontract.DomainContainerImageIdentity, Consumer: reducercontract.DomainSupplyChainImpact},
	}
	if got := CompletionEdges(); !slices.Equal(got, want) {
		t.Fatalf("CompletionEdges() = %v, want %v", got, want)
	}
}

func TestCompletionEdgesFormUniqueDAG(t *testing.T) {
	t.Parallel()
	edges := CompletionEdges()
	seen := make(map[CompletionEdge]struct{}, len(edges))
	adjacency := make(map[reducercontract.Domain][]reducercontract.Domain)
	for _, edge := range edges {
		if edge.Producer == edge.Consumer {
			t.Fatalf("completion dependency contains self-edge %v", edge)
		}
		if _, duplicate := seen[edge]; duplicate {
			t.Fatalf("completion dependency contains duplicate edge %v", edge)
		}
		seen[edge] = struct{}{}
		adjacency[edge.Producer] = append(adjacency[edge.Producer], edge.Consumer)
	}
	visiting := make(map[reducercontract.Domain]bool)
	visited := make(map[reducercontract.Domain]bool)
	var visit func(reducercontract.Domain) bool
	visit = func(domain reducercontract.Domain) bool {
		if visiting[domain] {
			return false
		}
		if visited[domain] {
			return true
		}
		visiting[domain] = true
		for _, consumer := range adjacency[domain] {
			if !visit(consumer) {
				return false
			}
		}
		visiting[domain] = false
		visited[domain] = true
		return true
	}
	for producer := range adjacency {
		if !visit(producer) {
			t.Fatalf("completion dependency catalog contains a cycle through %s", producer)
		}
	}
}

// TestConsumerDomainsExposesEveryCatalogConsumer proves the exported accessor
// reports exactly the catalog's consumer keys, in a stable order. The
// accessor exists so a consumer of this package -- the storage layer's
// cross-scope correlation reopen list -- can assert its own coverage against
// the catalog instead of restating the same constants and comparing them to
// themselves. If the accessor silently dropped or reordered a key, that
// downstream coverage assertion would go quietly false-green again.
func TestConsumerDomainsExposesEveryCatalogConsumer(t *testing.T) {
	t.Parallel()

	catalog := dependencyCatalog()
	got := ConsumerDomains()
	if len(got) != len(catalog) {
		t.Fatalf("ConsumerDomains() = %v (%d domains), want the %d catalog consumers", got, len(got), len(catalog))
	}
	for consumer := range catalog {
		if !slices.Contains(got, consumer) {
			t.Errorf("ConsumerDomains() = %v, missing catalog consumer %q", got, consumer)
		}
	}
	if !slices.IsSorted(got) {
		t.Errorf("ConsumerDomains() = %v, want a sorted (map-iteration-independent) order", got)
	}
}
