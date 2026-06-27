// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestAnsiblePlatformPlaybooksB7FixtureResolves guards the B-7 corpus fixtures
// tests/fixtures/ecosystems/ansible-platform-playbooks and ansible-shared-roles
// against drift. It reads the committed playbook, runs the same
// discovery+resolution the pipeline runs, and asserts the role reference
// resolves to the in-corpus ansible-shared-roles repository as a DEPENDS_ON edge
// carrying ANSIBLE_ROLE_REFERENCE evidence. The golden-corpus gate asserts the
// same edge on the real graph backend, filtered by that evidence kind; this test
// is the fast, Docker-free proof that the fixture content produces the edge the
// gate requires.
func TestAnsiblePlatformPlaybooksB7FixtureResolves(t *testing.T) {
	t.Parallel()

	playbookPath := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems",
		"ansible-platform-playbooks", "playbooks", "site.yml")
	content, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("read B-7 ansible fixture: %v", err)
	}

	envelopes := []facts.Envelope{
		{
			ScopeID: "ansible-platform-playbooks",
			Payload: map[string]any{
				"relative_path": "playbooks/site.yml",
				"content":       string(content),
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "ansible-shared-roles", Aliases: []string{"ansible-shared-roles"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	var roleEvidence *EvidenceFact
	for i := range evidence {
		if evidence[i].EvidenceKind == EvidenceKindAnsibleRoleReference &&
			evidence[i].TargetRepoID == "ansible-shared-roles" {
			roleEvidence = &evidence[i]
			break
		}
	}
	if roleEvidence == nil {
		t.Fatalf("fixture produced no ANSIBLE_ROLE_REFERENCE evidence resolving to ansible-shared-roles; got %+v", evidence)
	}
	if roleEvidence.RelationshipType != RelDependsOn {
		t.Fatalf("evidence relationship = %q, want %q", roleEvidence.RelationshipType, RelDependsOn)
	}

	_, resolved := Resolve(evidence, nil, 0)
	found := false
	for _, r := range resolved {
		if r.RelationshipType == RelDependsOn &&
			r.SourceRepoID == "ansible-platform-playbooks" &&
			r.TargetRepoID == "ansible-shared-roles" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture did not resolve a DEPENDS_ON ansible-platform-playbooks -> ansible-shared-roles; resolved=%+v", resolved)
	}
}
