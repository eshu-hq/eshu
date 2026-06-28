// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSaltFormulasB7FixtureResolves guards the B-7 corpus fixture
// tests/fixtures/ecosystems/salt-formulas against drift. It reads the committed
// Salt master config, runs the same discovery+resolution the pipeline runs, and
// asserts the Salt gitfs formula source resolves to the in-corpus
// deployable-source repository as a DEPENDS_ON edge carrying SALT_FORMULA_REFERENCE
// evidence. The golden-corpus gate's rc-36 asserts the same edge on the real
// graph backend, filtered by that evidence kind; this test is the fast,
// Docker-free proof that the fixture content actually produces the edge the gate
// requires.
func TestSaltFormulasB7FixtureResolves(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems",
		"salt-formulas", "master.yaml")
	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read B-7 salt fixture: %v", err)
	}

	envelopes := []facts.Envelope{
		{
			ScopeID: "salt-formulas",
			Payload: map[string]any{
				"relative_path": "master.yaml",
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

	var saltEvidence *EvidenceFact
	for i := range evidence {
		if evidence[i].EvidenceKind == EvidenceKindSaltFormulaReference &&
			evidence[i].TargetRepoID == "deployable-source" {
			saltEvidence = &evidence[i]
			break
		}
	}
	if saltEvidence == nil {
		t.Fatalf("fixture produced no SALT_FORMULA_REFERENCE evidence resolving to deployable-source; got %+v", evidence)
	}
	if saltEvidence.RelationshipType != RelDependsOn {
		t.Fatalf("evidence relationship = %q, want %q", saltEvidence.RelationshipType, RelDependsOn)
	}

	_, resolved := Resolve(evidence, nil, 0)
	found := false
	for _, r := range resolved {
		if r.RelationshipType == RelDependsOn &&
			r.SourceRepoID == "salt-formulas" &&
			r.TargetRepoID == "deployable-source" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture did not resolve a DEPENDS_ON salt-formulas -> deployable-source; resolved=%+v", resolved)
	}
}
