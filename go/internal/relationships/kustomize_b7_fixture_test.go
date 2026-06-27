// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestKustomizeDeployableOverlayB7FixtureResolves guards the B-7 corpus fixture
// tests/fixtures/ecosystems/kustomize-deployable-overlay against drift. It reads
// the committed kustomization.yaml, runs the same discovery+resolution the
// pipeline runs, and asserts the Kustomize remote-base reference resolves to the
// in-corpus deployable-source repository as a DEPLOYS_FROM edge carrying
// KUSTOMIZE_RESOURCE_REFERENCE evidence. The golden-corpus gate's rc-29 asserts
// the same edge on the real graph backend, filtered by that evidence kind; this
// test is the fast, Docker-free proof that the fixture content actually produces
// the edge the gate requires.
func TestKustomizeDeployableOverlayB7FixtureResolves(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems",
		"kustomize-deployable-overlay", "kustomization.yaml")
	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read B-7 kustomize fixture: %v", err)
	}

	envelopes := []facts.Envelope{
		{
			ScopeID: "kustomize-deployable-overlay",
			Payload: map[string]any{
				"relative_path": "kustomization.yaml",
				"content":       string(content),
			},
		},
	}
	// The in-corpus target. The gate synthesises the deployable-source remote as
	// github.com/acme/deployable-source, so its catalog alias is the slug.
	catalog := []CatalogEntry{
		{RepoID: "deployable-source", Aliases: []string{"deployable-source"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	var kustomizeEvidence *EvidenceFact
	for i := range evidence {
		if evidence[i].EvidenceKind == EvidenceKindKustomizeResource &&
			evidence[i].TargetRepoID == "deployable-source" {
			kustomizeEvidence = &evidence[i]
			break
		}
	}
	if kustomizeEvidence == nil {
		t.Fatalf("fixture produced no KUSTOMIZE_RESOURCE_REFERENCE evidence resolving to deployable-source; got %+v", evidence)
	}
	if kustomizeEvidence.RelationshipType != RelDeploysFrom {
		t.Fatalf("evidence relationship = %q, want %q", kustomizeEvidence.RelationshipType, RelDeploysFrom)
	}

	_, resolved := Resolve(evidence, nil, 0)
	found := false
	for _, r := range resolved {
		if r.RelationshipType == RelDeploysFrom &&
			r.SourceRepoID == "kustomize-deployable-overlay" &&
			r.TargetRepoID == "deployable-source" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture did not resolve a DEPLOYS_FROM kustomize-deployable-overlay -> deployable-source; resolved=%+v", resolved)
	}
}
