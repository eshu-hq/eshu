// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func rc29() goldengate.RequiredCorrelation {
	return goldengate.RequiredCorrelation{
		ID:            "rc-29",
		Relationship:  "DEPLOYS_FROM",
		FromLabel:     "Repository",
		ToLabel:       "Repository",
		MinimumCount:  1,
		EvidenceKinds: []string{"KUSTOMIZE_RESOURCE_REFERENCE"},
	}
}

func TestRepositoryCatalogDerivesFromRepositoryFacts(t *testing.T) {
	t.Parallel()

	odu := kustomizeDeploysFromOdu().Odu
	catalog := RepositoryCatalog(odu.Facts)
	if len(catalog) != 1 {
		t.Fatalf("catalog = %+v, want one repository entry", catalog)
	}
	if catalog[0].RepoID != "repo-payments" {
		t.Errorf("RepoID = %q, want repo-payments", catalog[0].RepoID)
	}
	found := false
	for _, alias := range catalog[0].Aliases {
		if alias == "payments-service" {
			found = true
		}
	}
	if !found {
		t.Errorf("aliases = %v, want to include payments-service", catalog[0].Aliases)
	}
}

func TestRepositoryCatalogDedupesByRepoID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: repositoryFactKind, Payload: map[string]any{"repo_id": "repo-a", "name": "first"}},
		{FactKind: repositoryFactKind, Payload: map[string]any{"repo_id": "repo-a", "name": "second"}},
	}
	catalog := RepositoryCatalog(envelopes)
	if len(catalog) != 1 {
		t.Fatalf("catalog = %+v, want deduped to one entry", catalog)
	}
	if catalog[0].Aliases[len(catalog[0].Aliases)-1] != "first" {
		t.Errorf("expected the first-seen envelope to win the dedupe, got %+v", catalog[0])
	}
}

func TestDiscoveredEvidenceKustomizeOdu(t *testing.T) {
	t.Parallel()

	ev := DiscoveredEvidence(kustomizeDeploysFromOdu().Odu)
	if !hasEvidence(ev, relationships.EvidenceKindKustomizeResource, relationships.RelDeploysFrom) {
		t.Fatalf("evidence = %+v, want KUSTOMIZE_RESOURCE_REFERENCE on DEPLOYS_FROM", ev)
	}
}

func TestDiscoveredEvidenceArgoCDOdu(t *testing.T) {
	t.Parallel()

	ev := DiscoveredEvidence(argocdDeploysFromOdu().Odu)
	if !hasEvidence(ev, relationships.EvidenceKindArgoCDAppSource, relationships.RelDeploysFrom) {
		t.Fatalf("evidence = %+v, want ARGOCD_APPLICATION_SOURCE on DEPLOYS_FROM", ev)
	}
}

func TestDiscoveredEvidenceEmptyCatalogProducesNoEvidence(t *testing.T) {
	t.Parallel()

	odu := Odu{
		Name: "odu:no-catalog",
		Facts: []facts.Envelope{
			{
				ScopeID:  "repo-deploy",
				FactKind: contentFactKind,
				Payload: map[string]any{
					"relative_path": "overlays/prod/kustomization.yaml",
					"content":       "resources:\n  - ../../base\nnamePrefix: payments-service\n",
				},
			},
		},
	}
	ev := DiscoveredEvidence(odu)
	if len(ev) != 0 {
		t.Errorf("evidence = %+v, want none: no repository fact means no catalog to match against", ev)
	}
}

func TestEvidenceSatisfiesKustomizePasses(t *testing.T) {
	t.Parallel()

	ev := DiscoveredEvidence(kustomizeDeploysFromOdu().Odu)
	ok, detail := EvidenceSatisfies(rc29(), ev)
	if !ok {
		t.Fatalf("EvidenceSatisfies(rc-29, kustomize) = false, detail=%q", detail)
	}
}

func TestEvidenceSatisfiesArgoCDFailsWithMissingKindDetail(t *testing.T) {
	t.Parallel()

	ev := DiscoveredEvidence(argocdDeploysFromOdu().Odu)
	ok, detail := EvidenceSatisfies(rc29(), ev)
	if ok {
		t.Fatal("EvidenceSatisfies(rc-29, argocd) = true, want false (wrong evidence kind)")
	}
	if !strings.Contains(detail, "KUSTOMIZE_RESOURCE_REFERENCE") {
		t.Errorf("detail = %q, want it to name the missing evidence kind", detail)
	}
}

func hasEvidence(ev []relationships.EvidenceFact, kind relationships.EvidenceKind, rel relationships.RelationshipType) bool {
	for _, e := range ev {
		if e.EvidenceKind == kind && e.RelationshipType == rel {
			return true
		}
	}
	return false
}
