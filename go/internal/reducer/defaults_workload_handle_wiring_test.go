// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

// stubWorkloadSiblingLookup returns one canned sibling for every name.
type stubWorkloadSiblingLookup struct {
	sibling WorkloadSibling
}

func (s stubWorkloadSiblingLookup) ListWorkloadSiblings(
	_ context.Context,
	_ []string,
) ([]WorkloadSibling, error) {
	return []WorkloadSibling{s.sibling}, nil
}

// TestHandleLadderRegistrationCarriesSiblingLookup guards the wiring, not
// the logic.
//
// The handle ladder is only worth anything if the four provider-asserted
// consumers actually carry the sibling-lookup seam. A ladder that is correct
// in isolation and unwired in DefaultHandlers ships inert: every unit test
// passes, production falls back to legacy pass-through rows, and only a
// two-tenant collision (or nobody) notices. This test fails if the seam is
// dropped from any registration. Mirrors
// TestDeploymentMappingRegistrationCarriesScopeRepos.
func TestHandleLadderRegistrationCarriesSiblingLookup(t *testing.T) {
	t.Parallel()

	sibling := WorkloadSibling{ID: "workload:repository:r_a:checkout", Name: "checkout", RepoID: "repository:r_a"}
	repos := &stubScopeReposReader{repos: []string{"repository:r_a"}}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                          &stubFactLoader{},
		WorkloadCloudRelationshipEdgeWriter: &recordingWorkloadCloudRelationshipWriter{},
		DocumentationEdgeWriter:             &fakeEdgeWriter{},
		ServiceCatalogCorrelationWriter:     &recordingServiceCatalogCorrelationWriter{},
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SupplyChainImpactWriter:             &recordingSupplyChainImpactWriter{},
			SupplyChainImpactFencingTokenIssuer: fixedSupplyChainImpactFencingTokenIssuer{token: 1},
		},
		ScopeRepos:            repos,
		WorkloadSiblingLookup: stubWorkloadSiblingLookup{sibling: sibling},
	})

	ctx := context.Background()
	assertSibling := func(t *testing.T, domain reducercontract.Domain, lookup handle.Lookup) {
		t.Helper()
		if lookup == nil {
			t.Fatalf("%s handler WorkloadLookup is nil: the handle ladder would ship inert on legacy pass-through", domain)
		}
		candidates, err := lookup.ListWorkloadHandleCandidates(ctx, []string{"checkout"})
		if err != nil {
			t.Fatalf("%s ListWorkloadHandleCandidates() error = %v", domain, err)
		}
		if len(candidates) != 1 || candidates[0].ID != sibling.ID {
			t.Fatalf("%s candidates = %+v, want the wired sibling %q: the registration wrapped a different seam", domain, candidates, sibling.ID)
		}
	}

	found := map[reducercontract.Domain]bool{}
	for _, definition := range definitions {
		switch definition.Domain {
		case DomainWorkloadCloudRelationshipMaterialization:
			handler, ok := definition.Handler.(WorkloadCloudRelationshipMaterializationHandler)
			if !ok {
				t.Fatalf("handler for %s = %T, want WorkloadCloudRelationshipMaterializationHandler", definition.Domain, definition.Handler)
			}
			if handler.ScopeRepos != handle.ScopeRepos(repos) {
				t.Fatal("cloud handler ScopeRepos is not the reader passed to DefaultHandlers")
			}
			if handler.WorkloadLookup == nil {
				t.Fatal("cloud handler WorkloadLookup is nil: the handle ladder would ship inert on legacy pass-through")
			}
			siblings, err := handler.WorkloadLookup.ListWorkloadSiblings(ctx, []string{"checkout"})
			if err != nil {
				t.Fatalf("cloud ListWorkloadSiblings() error = %v", err)
			}
			if len(siblings) != 1 || siblings[0].ID != sibling.ID {
				t.Fatalf("cloud siblings = %+v, want the wired sibling", siblings)
			}
			found[definition.Domain] = true
		case DomainDocumentationMaterialization:
			handler, ok := definition.Handler.(DocumentationEdgeMaterializationHandler)
			if !ok {
				t.Fatalf("handler for %s = %T, want DocumentationEdgeMaterializationHandler", definition.Domain, definition.Handler)
			}
			if handler.ScopeRepos != handle.ScopeRepos(repos) {
				t.Fatal("documentation handler ScopeRepos is not the reader passed to DefaultHandlers")
			}
			if handler.WorkloadLookup == nil {
				t.Fatal("documentation handler WorkloadLookup is nil: the handle ladder would ship inert on legacy pass-through")
			}
			siblings, err := handler.WorkloadLookup.ListWorkloadSiblings(ctx, []string{"checkout"})
			if err != nil {
				t.Fatalf("documentation ListWorkloadSiblings() error = %v", err)
			}
			if len(siblings) != 1 || siblings[0].ID != sibling.ID {
				t.Fatalf("documentation siblings = %+v, want the wired sibling", siblings)
			}
			found[definition.Domain] = true
		case DomainServiceCatalogCorrelation:
			handler, ok := definition.Handler.(ServiceCatalogCorrelationHandler)
			if !ok {
				t.Fatalf("handler for %s = %T, want ServiceCatalogCorrelationHandler", definition.Domain, definition.Handler)
			}
			if handler.ScopeRepos != handle.ScopeRepos(repos) {
				t.Fatal("service-catalog handler ScopeRepos is not the reader passed to DefaultHandlers")
			}
			assertSibling(t, definition.Domain, handler.WorkloadLookup)
			found[definition.Domain] = true
		case DomainSupplyChainImpact:
			handler, ok := definition.Handler.(supplychaincore.SupplyChainImpactHandler)
			if !ok {
				t.Fatalf("handler for %s = %T, want SupplyChainImpactHandler", definition.Domain, definition.Handler)
			}
			if handler.ScopeRepos != handle.ScopeRepos(repos) {
				t.Fatal("supply-chain handler ScopeRepos is not the reader passed to DefaultHandlers")
			}
			assertSibling(t, definition.Domain, handler.WorkloadLookup)
			found[definition.Domain] = true
		}
	}
	for _, domain := range []reducercontract.Domain{
		DomainWorkloadCloudRelationshipMaterialization,
		DomainDocumentationMaterialization,
		DomainServiceCatalogCorrelation,
		DomainSupplyChainImpact,
	} {
		if !found[domain] {
			t.Fatalf("no %s registration found", domain)
		}
	}
}

// TestHandleLadderRegistrationKeepsLegacyPassThroughWithoutLookup pins the
// nil default: without a sibling lookup every consumer keeps legacy rows,
// so a binary without a graph read path behaves exactly as before.
func TestHandleLadderRegistrationKeepsLegacyPassThroughWithoutLookup(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                          &stubFactLoader{},
		WorkloadCloudRelationshipEdgeWriter: &recordingWorkloadCloudRelationshipWriter{},
		DocumentationEdgeWriter:             &fakeEdgeWriter{},
		ServiceCatalogCorrelationWriter:     &recordingServiceCatalogCorrelationWriter{},
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SupplyChainImpactWriter:             &recordingSupplyChainImpactWriter{},
			SupplyChainImpactFencingTokenIssuer: fixedSupplyChainImpactFencingTokenIssuer{token: 1},
		},
		ScopeRepos: &stubScopeReposReader{},
	})

	for _, definition := range definitions {
		switch definition.Domain {
		case DomainWorkloadCloudRelationshipMaterialization:
			handler := definition.Handler.(WorkloadCloudRelationshipMaterializationHandler)
			if handler.WorkloadLookup != nil {
				t.Fatal("cloud handler WorkloadLookup non-nil without the dep: legacy pass-through would silently become miss-everything")
			}
		case DomainDocumentationMaterialization:
			handler := definition.Handler.(DocumentationEdgeMaterializationHandler)
			if handler.WorkloadLookup != nil {
				t.Fatal("documentation handler WorkloadLookup non-nil without the dep")
			}
		case DomainServiceCatalogCorrelation:
			handler := definition.Handler.(ServiceCatalogCorrelationHandler)
			if handler.WorkloadLookup != nil {
				t.Fatal("service-catalog handler WorkloadLookup non-nil without the dep")
			}
		case DomainSupplyChainImpact:
			handler := definition.Handler.(supplychaincore.SupplyChainImpactHandler)
			if handler.WorkloadLookup != nil {
				t.Fatal("supply-chain handler WorkloadLookup non-nil without the dep")
			}
		}
	}
}
