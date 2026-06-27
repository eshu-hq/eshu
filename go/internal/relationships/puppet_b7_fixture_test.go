// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestPuppetPlatformModulesB7FixtureResolves guards the B-7 corpus fixture
// tests/fixtures/ecosystems/puppet-platform-modules against drift. It reads the
// committed Puppetfile, runs the same discovery+resolution the pipeline runs,
// and asserts the Puppet module git source resolves to the in-corpus
// deployable-source repository as a DEPENDS_ON edge carrying
// PUPPET_MODULE_REFERENCE evidence. The golden-corpus gate's rc-32 asserts the
// same edge on the real graph backend, filtered by that evidence kind; this test
// is the fast, Docker-free proof that the fixture content actually produces the
// edge the gate requires.
func TestPuppetPlatformModulesB7FixtureResolves(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems",
		"puppet-platform-modules", "Puppetfile")
	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read B-7 puppet fixture: %v", err)
	}

	envelopes := []facts.Envelope{
		{
			ScopeID: "puppet-platform-modules",
			Payload: map[string]any{
				"relative_path": "Puppetfile",
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

	var puppetEvidence *EvidenceFact
	for i := range evidence {
		if evidence[i].EvidenceKind == EvidenceKindPuppetModuleReference &&
			evidence[i].TargetRepoID == "deployable-source" {
			puppetEvidence = &evidence[i]
			break
		}
	}
	if puppetEvidence == nil {
		t.Fatalf("fixture produced no PUPPET_MODULE_REFERENCE evidence resolving to deployable-source; got %+v", evidence)
	}
	if puppetEvidence.RelationshipType != RelDependsOn {
		t.Fatalf("evidence relationship = %q, want %q", puppetEvidence.RelationshipType, RelDependsOn)
	}

	_, resolved := Resolve(evidence, nil, 0)
	found := false
	for _, r := range resolved {
		if r.RelationshipType == RelDependsOn &&
			r.SourceRepoID == "puppet-platform-modules" &&
			r.TargetRepoID == "deployable-source" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture did not resolve a DEPENDS_ON puppet-platform-modules -> deployable-source; resolved=%+v", resolved)
	}
}
