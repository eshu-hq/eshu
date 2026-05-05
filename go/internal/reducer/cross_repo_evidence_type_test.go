package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestResolvedRelationshipEvidenceTypeUsesPreviewKind(t *testing.T) {
	t.Parallel()

	relationship := relationships.ResolvedRelationship{
		Details: map[string]any{
			"evidence_preview": []map[string]any{
				{"kind": string(relationships.EvidenceKindGitHubActionsReusableWorkflow)},
			},
			"evidence_kinds": []string{string(relationships.EvidenceKindTerraformModuleSource)},
		},
	}

	if got, want := resolvedRelationshipEvidenceType(relationship), "github_actions_reusable_workflow_ref"; got != want {
		t.Fatalf("resolvedRelationshipEvidenceType() = %q, want %q", got, want)
	}
}

func TestResolvedRelationshipEvidenceTypeFallsBackToEvidenceKinds(t *testing.T) {
	t.Parallel()

	relationship := relationships.ResolvedRelationship{
		Details: map[string]any{
			"evidence_kinds": []string{string(relationships.EvidenceKindTerraformModuleSource)},
		},
	}

	if got, want := resolvedRelationshipEvidenceType(relationship), "terraform_module_source"; got != want {
		t.Fatalf("resolvedRelationshipEvidenceType() = %q, want %q", got, want)
	}
}

func TestNormalizeEvidenceKindMapsGitHubActionsActionRepository(t *testing.T) {
	t.Parallel()

	if got, want := normalizeEvidenceKind(string(relationships.EvidenceKindGitHubActionsActionRepository)), "github_actions_action_repository"; got != want {
		t.Fatalf("normalizeEvidenceKind() = %q, want %q", got, want)
	}
}

func TestNormalizeEvidenceKindMapsGitHubActionsLocalReusableWorkflow(t *testing.T) {
	t.Parallel()

	if got, want := normalizeEvidenceKind(string(relationships.EvidenceKindGitHubActionsLocalReusableWorkflow)), "github_actions_local_reusable_workflow_ref"; got != want {
		t.Fatalf("normalizeEvidenceKind() = %q, want %q", got, want)
	}
}

func TestNormalizeEvidenceKindFallsBackToLowercaseConstant(t *testing.T) {
	t.Parallel()

	if got, want := normalizeEvidenceKind("TERRAFORM_WAFV2_WEB_ACL"), "terraform_wafv2_web_acl"; got != want {
		t.Fatalf("normalizeEvidenceKind() = %q, want %q", got, want)
	}
}
