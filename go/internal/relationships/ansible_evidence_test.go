package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDiscoverAnsibleRoleReferenceEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-ops",
			Payload: map[string]any{
				"artifact_type": "ansible_playbook",
				"relative_path": "playbooks/site.yml",
				"content": `- hosts: all
  roles:
    - role: payments-service
      src: git+https://github.com/myorg/payments-service.git
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindAnsibleRoleReference {
		t.Fatalf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindAnsibleRoleReference)
	}
	if evidence[0].RelationshipType != RelDependsOn {
		t.Fatalf("type = %q, want %q", evidence[0].RelationshipType, RelDependsOn)
	}
	if evidence[0].TargetRepoID != "repo-payments" {
		t.Fatalf("target = %q, want repo-payments", evidence[0].TargetRepoID)
	}
}

func TestDiscoverAnsibleImportPlaybookEvidenceStaysOnPlaybooks(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-ops",
			Payload: map[string]any{
				"artifact_type": "ansible_playbook",
				"relative_path": "playbooks/site.yml",
				"content": `- hosts: all
  import_playbook: payments-service.yml
`,
			},
		},
		{
			ScopeID: "repo-ops",
			Payload: map[string]any{
				"artifact_type": "ansible_inventory",
				"relative_path": "inventories/prod.yml",
				"content": `all:
  vars:
    import_playbook: payments-service.yml
`,
			},
		},
		{
			ScopeID: "repo-ops",
			Payload: map[string]any{
				"artifact_type": "ansible_vars",
				"relative_path": "group_vars/all.yml",
				"content": `import_playbook: payments-service.yml
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindAnsibleRoleReference {
		t.Fatalf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindAnsibleRoleReference)
	}
	if evidence[0].RelationshipType != RelDependsOn {
		t.Fatalf("type = %q, want %q", evidence[0].RelationshipType, RelDependsOn)
	}
	if evidence[0].TargetRepoID != "repo-payments" {
		t.Fatalf("target = %q, want repo-payments", evidence[0].TargetRepoID)
	}
	if got, want := evidence[0].Details["reference_key"], "import_playbook"; got != want {
		t.Fatalf("reference_key = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["source_ref"], "payments-service.yml"; got != want {
		t.Fatalf("source_ref = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["path"], "playbooks/site.yml"; got != want {
		t.Fatalf("path = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["first_party_ref_kind"], "ansible_import_playbook"; got != want {
		t.Fatalf("first_party_ref_kind = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["first_party_ref_name"], "payments-service"; got != want {
		t.Fatalf("first_party_ref_name = %#v, want %#v", got, want)
	}
}
