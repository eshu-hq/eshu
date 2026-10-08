// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestFluxEvidenceIdentityIsPostgresSafeAndRepositoryScoped is the issue
// #7543 regression: two source repositories carrying the same Flux
// namespace/name and a common target must produce two candidates with
// distinct repository attribution, and the emitted SourceEntityID must be
// PostgreSQL-safe (no NUL bytes) and repository-scoped.
func TestFluxEvidenceIdentityIsPostgresSafeAndRepositoryScoped(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		fluxGitRepositoryEnvelope("repo-config-a", "sources.yaml", "flux-system", "app-source",
			"https://github.com/myorg/payments-deploy.git"),
		fluxGitRepositoryEnvelope("repo-config-b", "sources.yaml", "flux-system", "app-source",
			"https://github.com/myorg/payments-deploy.git"),
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config-a", RemoteURL: "https://github.com/myorg/gitops-config-a"},
		{RepoID: "repo-config-b", RemoteURL: "https://github.com/myorg/gitops-config-b"},
		{RepoID: "repo-deploy", RemoteURL: "https://github.com/myorg/payments-deploy"},
	}

	evidence, _ := DiscoverEvidenceWithStats(envelopes, catalog)
	var flux []EvidenceFact
	for _, fact := range evidence {
		if fact.EvidenceKind == EvidenceKindFluxGitRepositorySource {
			flux = append(flux, fact)
		}
	}
	if len(flux) != 2 {
		t.Fatalf("flux evidence count = %d, want 2: %#v", len(flux), flux)
	}
	for _, fact := range flux {
		if strings.ContainsRune(fact.SourceEntityID, 0) {
			t.Fatalf("SourceEntityID = %q, must not contain NUL", fact.SourceEntityID)
		}
	}
	if flux[0].SourceEntityID == flux[1].SourceEntityID {
		t.Fatalf("SourceEntityID = %q for both sources, want repository-scoped identities",
			flux[0].SourceEntityID)
	}

	candidates, resolved := Resolve(flux, nil, 0)
	if len(candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2 (one per source repository): %#v",
			len(candidates), candidates)
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		seen[c.SourceRepoID] = true
		if c.TargetRepoID != "repo-deploy" {
			t.Fatalf("candidate target = %q, want repo-deploy", c.TargetRepoID)
		}
	}
	if !seen["repo-config-a"] || !seen["repo-config-b"] {
		t.Fatalf("candidate sources = %v, want both repo-config-a and repo-config-b", seen)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved count = %d, want 2: %#v", len(resolved), resolved)
	}
}
