// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

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
// a typo in the catalog fails here rather than silently disabling the future
// readiness gate.
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
// present in the standalone map. The readiness/re-enqueue slices read the
// dependency off the registered DomainDefinition, so an unwired constructor
// would discover no producer and permit the early empty-join execution this
// contract prevents (#5709 review).
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
