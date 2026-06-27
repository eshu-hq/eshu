// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestChefCookbooksB7FixtureResolves guards the B-7 corpus fixture
// tests/fixtures/ecosystems/chef-cookbooks against drift. It reads the committed
// Berksfile, runs the same discovery+resolution the pipeline runs, and asserts
// the Chef cookbook git source resolves to the in-corpus deployable-source
// repository as a DEPENDS_ON edge carrying CHEF_COOKBOOK_DEPENDENCY evidence. The
// golden-corpus gate's rc-33 asserts the same edge on the real graph backend,
// filtered by that evidence kind; this test is the fast, Docker-free proof that
// the fixture content actually produces the edge the gate requires.
func TestChefCookbooksB7FixtureResolves(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems",
		"chef-cookbooks", "Berksfile")
	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read B-7 chef fixture: %v", err)
	}

	envelopes := []facts.Envelope{
		{
			ScopeID: "chef-cookbooks",
			Payload: map[string]any{
				"relative_path": "Berksfile",
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

	var chefEvidence *EvidenceFact
	for i := range evidence {
		if evidence[i].EvidenceKind == EvidenceKindChefCookbookDependency &&
			evidence[i].TargetRepoID == "deployable-source" {
			chefEvidence = &evidence[i]
			break
		}
	}
	if chefEvidence == nil {
		t.Fatalf("fixture produced no CHEF_COOKBOOK_DEPENDENCY evidence resolving to deployable-source; got %+v", evidence)
	}
	if chefEvidence.RelationshipType != RelDependsOn {
		t.Fatalf("evidence relationship = %q, want %q", chefEvidence.RelationshipType, RelDependsOn)
	}

	_, resolved := Resolve(evidence, nil, 0)
	found := false
	for _, r := range resolved {
		if r.RelationshipType == RelDependsOn &&
			r.SourceRepoID == "chef-cookbooks" &&
			r.TargetRepoID == "deployable-source" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture did not resolve a DEPENDS_ON chef-cookbooks -> deployable-source; resolved=%+v", resolved)
	}
}
