// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"
)

func TestCrossScopeDependencyValidate(t *testing.T) {
	t.Parallel()

	t.Run("empty producer set is rejected", func(t *testing.T) {
		t.Parallel()
		if err := (CrossScopeDependency{}).Validate(); err == nil {
			t.Fatal("empty cross-scope dependency must be rejected")
		}
	})

	t.Run("unregistered producer domain is rejected", func(t *testing.T) {
		t.Parallel()
		dep := CrossScopeDependency{ProducerDomains: []Domain{Domain("not_a_real_domain")}}
		if err := dep.Validate(); err == nil {
			t.Fatal("cross-scope dependency naming an unregistered producer must be rejected")
		}
	})

	t.Run("registered producer domain is accepted", func(t *testing.T) {
		t.Parallel()
		dep := CrossScopeDependency{ProducerDomains: []Domain{DomainContainerImageIdentity}}
		if err := dep.Validate(); err != nil {
			t.Fatalf("valid cross-scope dependency rejected: %v", err)
		}
	})
}

// TestCrossScopeDependencyCatalogIsValid asserts every entry in the single
// source of truth names a registered consumer and only registered producers, so
// a typo in the catalog fails here rather than silently disabling completion
// replay.
func TestCrossScopeDependencyCatalogIsValid(t *testing.T) {
	t.Parallel()

	catalog := crossScopeDependencyCatalog()
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

func TestCrossScopeCompletionEdgesExposeCatalogExactly(t *testing.T) {
	t.Parallel()
	want := []CrossScopeCompletionEdge{
		{Producer: DomainCICDRunCorrelation, Consumer: DomainSupplyChainImpact},
		{Producer: DomainContainerImageIdentity, Consumer: DomainCICDRunCorrelation},
		{Producer: DomainContainerImageIdentity, Consumer: DomainSupplyChainImpact},
	}
	if got := CrossScopeCompletionEdges(); !slices.Equal(got, want) {
		t.Fatalf("CrossScopeCompletionEdges() = %v, want %v", got, want)
	}
}

func TestCrossScopeCompletionEdgesFormUniqueDAG(t *testing.T) {
	t.Parallel()
	edges := CrossScopeCompletionEdges()
	seen := make(map[CrossScopeCompletionEdge]struct{}, len(edges))
	adjacency := make(map[Domain][]Domain)
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
	visiting := make(map[Domain]bool)
	visited := make(map[Domain]bool)
	var visit func(Domain) bool
	visit = func(domain Domain) bool {
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

// TestCrossScopeConsumerDomainsExposesEveryCatalogConsumer proves the exported
// accessor reports exactly the catalog's consumer keys, in a stable order. The
// accessor exists so a consumer of this package -- the storage layer's
// cross-scope correlation reopen list -- can assert its own coverage against
// the catalog instead of restating the same constants and comparing them to
// themselves. If the accessor silently dropped or reordered a key, that
// downstream coverage assertion would go quietly false-green again.
func TestCrossScopeConsumerDomainsExposesEveryCatalogConsumer(t *testing.T) {
	t.Parallel()

	catalog := crossScopeDependencyCatalog()
	got := CrossScopeConsumerDomains()
	if len(got) != len(catalog) {
		t.Fatalf("CrossScopeConsumerDomains() = %v (%d domains), want the %d catalog consumers", got, len(got), len(catalog))
	}
	for consumer := range catalog {
		if !slices.Contains(got, consumer) {
			t.Errorf("CrossScopeConsumerDomains() = %v, missing catalog consumer %q", got, consumer)
		}
	}
	if !slices.IsSorted(got) {
		t.Errorf("CrossScopeConsumerDomains() = %v, want a sorted (map-iteration-independent) order", got)
	}
}

// TestDomainDefinitionValidatesCrossScopeDependencies proves DomainDefinition
// registration rejects an otherwise-valid registered definition once an invalid
// cross-scope dependency is attached, exercising the Validate wiring against a
// real definition rather than a hand-built truth contract.
func TestDomainDefinitionValidatesCrossScopeDependencies(t *testing.T) {
	t.Parallel()

	definitions := DefaultDomainDefinitions()
	if len(definitions) == 0 {
		t.Fatal("expected at least one registered domain definition to borrow for the wiring test")
	}
	base := definitions[0]
	if err := base.Validate(); err != nil {
		t.Fatalf("borrowed base definition %q is not valid: %v", base.Domain, err)
	}

	base.CrossScopeDependencies = []CrossScopeDependency{{}}
	if err := base.Validate(); err == nil {
		t.Fatal("definition with an empty cross-scope dependency must be rejected")
	}
}

// TestCICDRunCorrelationDefinitionCarriesCatalogDependency proves the catalog is
// actually wired onto the registered ci_cd_run_correlation definition, not just
// present in the standalone map. The completion runner derives its fanout
// edges from the same dependency carried by the registered DomainDefinition,
// so an unwired constructor would let early empty-join output stand.
func TestCICDRunCorrelationDefinitionCarriesCatalogDependency(t *testing.T) {
	t.Parallel()

	def := cicdRunCorrelationDomainDefinition()
	if err := def.Validate(); err != nil {
		t.Fatalf("ci_cd_run_correlation definition is invalid: %v", err)
	}
	if len(def.CrossScopeDependencies) != 1 {
		t.Fatalf("ci_cd_run_correlation must declare exactly one cross-scope dependency, got %d", len(def.CrossScopeDependencies))
	}
	producers := def.CrossScopeDependencies[0].ProducerDomains
	if len(producers) != 1 || producers[0] != DomainContainerImageIdentity {
		t.Fatalf("ci_cd_run_correlation cross-scope producer = %v, want [%s]", producers, DomainContainerImageIdentity)
	}
}

// TestSupplyChainImpactDefinitionCarriesCatalogDependency pins the third link of
// the same chain. supply_chain_impact reads container_image_identity directly
// for its repository anchor and ci_cd_run_correlation for deployment context,
// and
// matchingSupplyChainDeployments rejects a correlation that has not yet resolved
// its artifact identity -- so a finding classified before that correlation
// commits keeps an empty environments list until producer completion schedules
// it again. The catalog and registered definition are both asserted here so
// scheduling and declared truth cannot drift.
func TestSupplyChainImpactDefinitionCarriesCatalogDependency(t *testing.T) {
	t.Parallel()

	def := supplyChainImpactDomainDefinition()
	if err := def.Validate(); err != nil {
		t.Fatalf("supply_chain_impact definition is invalid: %v", err)
	}
	if len(def.CrossScopeDependencies) != 1 {
		t.Fatalf("supply_chain_impact must declare exactly one cross-scope dependency, got %d", len(def.CrossScopeDependencies))
	}
	producers := def.CrossScopeDependencies[0].ProducerDomains
	want := []Domain{DomainContainerImageIdentity, DomainCICDRunCorrelation}
	if !slices.Equal(producers, want) {
		t.Fatalf("supply_chain_impact cross-scope producers = %v, want %v", producers, want)
	}
}
