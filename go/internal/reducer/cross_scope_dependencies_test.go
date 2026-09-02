// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// The cross-scope dependency catalog itself moved to [crossscope] (issue
// #6061); its own tests moved with it into crossscope/dependencies_test.go.
// What remains here is registry-wiring proof: that the catalog is actually
// reachable from a registered DomainDefinition, not just present in the
// standalone package, which only the reducer root can assert since it alone
// constructs domain definitions.

import (
	"slices"
	"testing"
)

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
