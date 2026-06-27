// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestSourceToolForEvidenceKind locks the EvidenceKind -> source_tool family
// collapse defined in docs/public/reference/edge-source-tool-provenance.md
// (#3998). One case per family plus the shared/ambiguous and unmapped cases.
func TestSourceToolForEvidenceKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind relationships.EvidenceKind
		want string
	}{
		{relationships.EvidenceKindTerraformAppRepo, "terraform"},
		{relationships.EvidenceKindTerraformConfigPath, "terraform"},
		{relationships.EvidenceKindTerraformModuleSource, "terraform"}, // shared with terragrunt; defaults to terraform (#3998 ¹)
		{relationships.EvidenceKindTerragruntDependencyConfigPath, "terragrunt"},
		{relationships.EvidenceKindHelmChart, "helm"},
		{relationships.EvidenceKindKustomizeResource, "kustomize"},
		{relationships.EvidenceKindKustomizeImage, "kustomize"},
		{relationships.EvidenceKindArgoCDAppSource, "argocd"},
		{relationships.EvidenceKindGitHubActionsReusableWorkflow, "github_actions"},
		{relationships.EvidenceKindJenkinsGitHubRepository, "jenkins"},
		{relationships.EvidenceKindDockerComposeImage, "docker_compose"},
		{relationships.EvidenceKindDockerfileSourceLabel, "docker"},
		{relationships.EvidenceKindAnsibleRoleReference, "ansible"},
		{relationships.EvidenceKindPuppetModuleReference, "puppet"},
		{relationships.EvidenceKindChefCookbookDependency, "chef"},
		{relationships.EvidenceKindGCPCloudRelationship, "gcp"},
	}
	for _, tc := range cases {
		if got := sourceToolForEvidenceKind(string(tc.kind)); got != tc.want {
			t.Errorf("sourceToolForEvidenceKind(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}

	// Every EvidenceKind that has an evidence_type mapping must also have a
	// source_tool mapping. evidenceKindToType is the existing complete enumeration
	// of cross-repo kinds; iterating it forces a new kind added there (required,
	// since evidence_type is mandatory) to also be classified here — a built-in
	// drift guard without a second hand-maintained list.
	for kind := range evidenceKindToType {
		if got := sourceToolForEvidenceKind(string(kind)); got == "" {
			t.Errorf("EvidenceKind %q has an evidence_type but no source_tool mapping; add it to evidenceKindToSourceTool", kind)
		}
	}

	// Generated/runtime kinds that are not named constants (the Terraform schema
	// extractor synthesizes TERRAFORM_<resource> at runtime) classify by family
	// prefix rather than falling through to unknown.
	for _, generated := range []string{"TERRAFORM_ECS_SERVICE", "TERRAFORM_WAFV2_WEB_ACL", "TERRAFORM_PAGERDUTY_SERVICE"} {
		if got := sourceToolForEvidenceKind(generated); got != "terraform" {
			t.Errorf("generated kind %q: got %q, want terraform", generated, got)
		}
	}

	// An unmapped kind in no known family still yields "" so the caller can decide
	// between absent and the explicit "unknown" token.
	if got := sourceToolForEvidenceKind("SOME_FUTURE_UNMAPPED_KIND"); got != "" {
		t.Errorf("unmapped kind: got %q, want empty", got)
	}
}

// TestResolvedRelationshipSourceTool proves the per-edge derivation mirrors the
// evidence_type primary-kind selection (preview kind first, then the first
// evidence_kinds entry) so source_tool and evidence_type always describe the
// same evidence. A Tier-2 edge with a present-but-unmapped primary kind is
// stamped the explicit "unknown" token; an edge with no evidence kind at all is
// not stamped.
func TestResolvedRelationshipSourceTool(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rel  relationships.ResolvedRelationship
		want string
	}{
		{
			name: "preview kind wins",
			rel: relationships.ResolvedRelationship{Details: map[string]any{
				"evidence_preview": []map[string]any{{"kind": string(relationships.EvidenceKindKustomizeResource)}},
				"evidence_kinds":   []string{string(relationships.EvidenceKindArgoCDAppSource)},
			}},
			want: "kustomize",
		},
		{
			name: "falls back to first evidence kind",
			rel: relationships.ResolvedRelationship{Details: map[string]any{
				"evidence_kinds": []string{string(relationships.EvidenceKindAnsibleRoleReference)},
			}},
			want: "ansible",
		},
		{
			name: "present but unmapped kind -> unknown",
			rel: relationships.ResolvedRelationship{Details: map[string]any{
				"evidence_kinds": []string{"SOME_FUTURE_UNMAPPED_KIND"},
			}},
			want: "unknown",
		},
		{
			name: "no evidence kind -> not stamped",
			rel:  relationships.ResolvedRelationship{Details: map[string]any{}},
			want: "",
		},
	}
	for _, tc := range cases {
		if got := resolvedRelationshipSourceTool(tc.rel); got != tc.want {
			t.Errorf("%s: resolvedRelationshipSourceTool() = %q, want %q", tc.name, got, tc.want)
		}
	}
}
