// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func fluxGitRepositoryEnvelope(sourceRepoID, filePath, namespace, name, url string) facts.Envelope {
	return facts.Envelope{
		ScopeID: sourceRepoID,
		Payload: map[string]any{
			"repo_id":       sourceRepoID,
			"relative_path": filePath,
			"parsed_file_data": map[string]any{
				"flux_git_repositories": []any{
					map[string]any{
						"name":      name,
						"namespace": namespace,
						"url":       url,
					},
				},
			},
		},
	}
}

func TestDiscoverStructuredFluxEvidenceUniqueURLMatchLinksAcrossRepos(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		fluxGitRepositoryEnvelope("repo-config", "clusters/prod/git-repository.yaml", "flux-system", "app-source",
			"https://github.com/myorg/payments-deploy.git"),
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", RemoteURL: "https://github.com/myorg/gitops-config"},
		{RepoID: "repo-deploy", RemoteURL: "https://github.com/myorg/payments-deploy"},
	}

	evidence, stats := DiscoverEvidenceWithStats(envelopes, catalog)
	fact, ok := findEvidenceByKind(evidence, EvidenceKindFluxGitRepositorySource)
	if !ok {
		t.Fatalf("missing FLUX_GIT_REPOSITORY_SOURCE evidence: %#v", evidence)
	}
	if fact.RelationshipType != RelDeploysFrom {
		t.Fatalf("relationship type = %q, want %q", fact.RelationshipType, RelDeploysFrom)
	}
	if fact.SourceRepoID != "repo-config" {
		t.Fatalf("source repo = %q, want repo-config", fact.SourceRepoID)
	}
	if fact.TargetRepoID != "repo-deploy" {
		t.Fatalf("target repo = %q, want repo-deploy", fact.TargetRepoID)
	}
	if fact.Confidence != DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindFluxGitRepositorySource) {
		t.Fatalf("confidence = %v, want registry value", fact.Confidence)
	}
	if got := fact.Details["flux_git_repository_name"]; got != "app-source" {
		t.Fatalf("details[flux_git_repository_name] = %#v, want app-source", got)
	}
	if got := fact.Details["flux_git_repository_namespace"]; got != "flux-system" {
		t.Fatalf("details[flux_git_repository_namespace] = %#v, want flux-system", got)
	}
	if got := fact.SourceEntityID; got != "FluxGitRepository\x00flux-system\x00app-source" {
		t.Fatalf("SourceEntityID = %q, want qualified identity", got)
	}
	if got := fact.Details["url"]; got != "https://github.com/myorg/payments-deploy.git" {
		t.Fatalf("details[url] = %#v, want raw url preserved", got)
	}

	if stats.FluxCrossRepoURLResolution.Linked != 1 {
		t.Fatalf("stats.Linked = %d, want 1: %#v", stats.FluxCrossRepoURLResolution.Linked, stats)
	}
	if stats.FluxCrossRepoURLResolution.Unresolved != 0 || stats.FluxCrossRepoURLResolution.Ambiguous != 0 || stats.FluxCrossRepoURLResolution.Self != 0 {
		t.Fatalf("stats has spurious non-linked outcomes: %#v", stats)
	}
}

func TestDiscoverStructuredFluxEvidenceDistinctNamespacesDoNotCollapse(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		fluxGitRepositoryEnvelope("repo-config", "sources.yaml", "team-a", "app-source", "https://github.com/myorg/payments-deploy.git"),
		fluxGitRepositoryEnvelope("repo-config", "sources.yaml", "team-b", "app-source", "https://github.com/myorg/payments-deploy.git"),
	}
	catalog := []CatalogEntry{{RepoID: "repo-deploy", RemoteURL: "https://github.com/myorg/payments-deploy"}}
	evidence, _ := DiscoverEvidenceWithStats(envelopes, catalog)
	var got []EvidenceFact
	for _, fact := range evidence {
		if fact.EvidenceKind == EvidenceKindFluxGitRepositorySource {
			got = append(got, fact)
		}
	}
	if len(got) != 2 {
		t.Fatalf("qualified evidence count = %d, want 2: %#v", len(got), got)
	}
}

func TestDiscoverStructuredFluxEvidenceUnresolvedURLTalliesUnresolvedAndEmitsNoEdge(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		fluxGitRepositoryEnvelope("repo-config", "clusters/prod/git-repository.yaml", "", "app-source",
			"https://github.com/myorg/never-indexed.git"),
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", RemoteURL: "https://github.com/myorg/gitops-config"},
		{RepoID: "repo-deploy", RemoteURL: "https://github.com/myorg/payments-deploy"},
	}

	evidence, stats := DiscoverEvidenceWithStats(envelopes, catalog)
	if _, ok := findEvidenceByKind(evidence, EvidenceKindFluxGitRepositorySource); ok {
		t.Fatalf("unexpected FLUX_GIT_REPOSITORY_SOURCE evidence for an unindexed url: %#v", evidence)
	}
	if stats.FluxCrossRepoURLResolution.Unresolved != 1 {
		t.Fatalf("stats.Unresolved = %d, want 1: %#v", stats.FluxCrossRepoURLResolution.Unresolved, stats)
	}
	if stats.FluxCrossRepoURLResolution.Linked != 0 || stats.FluxCrossRepoURLResolution.Ambiguous != 0 || stats.FluxCrossRepoURLResolution.Self != 0 {
		t.Fatalf("stats has spurious non-unresolved outcomes: %#v", stats)
	}
}

func TestDiscoverStructuredFluxEvidenceSelfReferenceSkipsAndTalliesSelf(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		fluxGitRepositoryEnvelope("repo-config", "clusters/prod/git-repository.yaml", "", "app-source",
			"https://github.com/myorg/gitops-config.git"),
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", RemoteURL: "https://github.com/myorg/gitops-config"},
		{RepoID: "repo-deploy", RemoteURL: "https://github.com/myorg/payments-deploy"},
	}

	evidence, stats := DiscoverEvidenceWithStats(envelopes, catalog)
	if _, ok := findEvidenceByKind(evidence, EvidenceKindFluxGitRepositorySource); ok {
		t.Fatalf("unexpected FLUX_GIT_REPOSITORY_SOURCE evidence for a same-repo GitRepository: %#v", evidence)
	}
	if stats.FluxCrossRepoURLResolution.Self != 1 {
		t.Fatalf("stats.Self = %d, want 1: %#v", stats.FluxCrossRepoURLResolution.Self, stats)
	}
	if stats.FluxCrossRepoURLResolution.Linked != 0 || stats.FluxCrossRepoURLResolution.Unresolved != 0 || stats.FluxCrossRepoURLResolution.Ambiguous != 0 {
		t.Fatalf("stats has spurious non-self outcomes: %#v", stats)
	}
}

func TestDiscoverStructuredFluxEvidenceAmbiguousMatchSkipsAndTalliesAmbiguous(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		fluxGitRepositoryEnvelope("repo-config", "clusters/prod/git-repository.yaml", "", "app-source",
			"https://github.com/myorg/payments-deploy.git"),
	}
	// Two catalog entries sharing the same normalized RemoteURL is
	// structurally near-impossible in production (RepoID derives from the
	// normalized URL and the catalog dedupes by RepoID), but the resolver
	// must never guess between them -- construct the case directly to prove
	// the never-fabricate guard.
	catalog := []CatalogEntry{
		{RepoID: "repo-config", RemoteURL: "https://github.com/myorg/gitops-config"},
		{RepoID: "repo-deploy-a", RemoteURL: "https://github.com/myorg/payments-deploy"},
		{RepoID: "repo-deploy-b", RemoteURL: "https://github.com/myorg/payments-deploy"},
	}

	evidence, stats := DiscoverEvidenceWithStats(envelopes, catalog)
	if _, ok := findEvidenceByKind(evidence, EvidenceKindFluxGitRepositorySource); ok {
		t.Fatalf("unexpected FLUX_GIT_REPOSITORY_SOURCE evidence for an ambiguous url: %#v", evidence)
	}
	if stats.FluxCrossRepoURLResolution.Ambiguous != 1 {
		t.Fatalf("stats.Ambiguous = %d, want 1: %#v", stats.FluxCrossRepoURLResolution.Ambiguous, stats)
	}
	if stats.FluxCrossRepoURLResolution.Linked != 0 || stats.FluxCrossRepoURLResolution.Unresolved != 0 || stats.FluxCrossRepoURLResolution.Self != 0 {
		t.Fatalf("stats has spurious non-ambiguous outcomes: %#v", stats)
	}
}

func TestDiscoverStructuredFluxEvidenceNeverResolvesOCIRepositoryURL(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-config",
			Payload: map[string]any{
				"repo_id":       "repo-config",
				"relative_path": "clusters/prod/oci-repository.yaml",
				"parsed_file_data": map[string]any{
					// An OCIRepository url lives in flux_oci_repositories, never
					// flux_git_repositories. Even though this url would
					// otherwise resolve cleanly, discoverStructuredFluxEvidence
					// must never read this bucket: an OCIRepository names a
					// registry, not a Repository node.
					"flux_oci_repositories": []any{
						map[string]any{
							"name": "app-oci-source",
							"url":  "oci://github.com/myorg/payments-deploy",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", RemoteURL: "https://github.com/myorg/gitops-config"},
		{RepoID: "repo-deploy", RemoteURL: "https://github.com/myorg/payments-deploy"},
	}

	evidence, stats := DiscoverEvidenceWithStats(envelopes, catalog)
	if _, ok := findEvidenceByKind(evidence, EvidenceKindFluxGitRepositorySource); ok {
		t.Fatalf("unexpected FLUX_GIT_REPOSITORY_SOURCE evidence from an OCIRepository url: %#v", evidence)
	}
	if stats.FluxCrossRepoURLResolution != (FluxCrossRepoURLResolutionStats{}) {
		t.Fatalf("stats should stay zero for an OCIRepository-only envelope, got %#v", stats.FluxCrossRepoURLResolution)
	}
}

func TestDiscoverStructuredFluxEvidenceNeverUsesFuzzyAliasMatch(t *testing.T) {
	t.Parallel()

	// The catalog's only entry has an alias that would fuzzy-match this url's
	// path token ("payments-deploy"), but its RemoteURL is a DIFFERENT host --
	// discoverStructuredFluxEvidence must ignore the alias and tally
	// unresolved, proving strict equality rather than matchCatalog's fuzzy
	// token match.
	envelopes := []facts.Envelope{
		fluxGitRepositoryEnvelope("repo-config", "clusters/prod/git-repository.yaml", "", "app-source",
			"https://gitlab.example.com/myorg/payments-deploy.git"),
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", RemoteURL: "https://github.com/myorg/gitops-config"},
		{
			RepoID:    "repo-deploy",
			Aliases:   []string{"payments-deploy"},
			RemoteURL: "https://github.com/myorg/payments-deploy",
		},
	}

	evidence, stats := DiscoverEvidenceWithStats(envelopes, catalog)
	if _, ok := findEvidenceByKind(evidence, EvidenceKindFluxGitRepositorySource); ok {
		t.Fatalf("unexpected FLUX_GIT_REPOSITORY_SOURCE evidence via alias fuzzy match: %#v", evidence)
	}
	if stats.FluxCrossRepoURLResolution.Unresolved != 1 {
		t.Fatalf("stats.Unresolved = %d, want 1: %#v", stats.FluxCrossRepoURLResolution.Unresolved, stats)
	}
}

func TestDiscoverEvidenceIgnoresStatsForExistingCallers(t *testing.T) {
	t.Parallel()

	// DiscoverEvidence's signature must stay unchanged for the many existing
	// callers (issue #5483 C2 design note): it still returns evidence facts
	// only, discarding DiscoveryStats.
	envelopes := []facts.Envelope{
		fluxGitRepositoryEnvelope("repo-config", "clusters/prod/git-repository.yaml", "", "app-source",
			"https://github.com/myorg/payments-deploy.git"),
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", RemoteURL: "https://github.com/myorg/gitops-config"},
		{RepoID: "repo-deploy", RemoteURL: "https://github.com/myorg/payments-deploy"},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if _, ok := findEvidenceByKind(evidence, EvidenceKindFluxGitRepositorySource); !ok {
		t.Fatalf("DiscoverEvidence() dropped the linked Flux evidence fact: %#v", evidence)
	}
}
