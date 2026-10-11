// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import "sort"

// nameScopedByEvidenceKind is the name-scope of every evidence_kind an
// extractor emits, as data. A name-scoped kind's path is built through
// appendConfigArtifactRow, so the repository name is stripped from it
// (StripRepositoryName). An unscoped kind's row is built by its extractor
// directly and its path never depends on the name. The table has exactly one
// value per kind: a kind that was in both families would make a stored row
// ambiguous, because the read-time name step would not know whether to strip.
//
// TestNameScopeTableMatchesTheRowConstructors reads both call-site families
// from source and fails when this table and the constructors disagree, or when
// an extractor emits a kind the table does not name.
var nameScopedByEvidenceKind = map[string]bool{
	// HCL (Terraform and Terragrunt), built by appendConfigArtifactRow.
	"terraform_var_file":                true,
	"terragrunt_dependency_config_path": true,
	"terragrunt_read_config":            true,
	"terragrunt_include_path":           true,
	"terragrunt_find_in_parent_folders": true,
	"local_config_asset":                true,
	"terraform_module_source_path":      true,
	// Docker Compose, built by appendConfigArtifactRow.
	"docker_compose_env_file":    true,
	"docker_compose_config_file": true,
	"docker_compose_secret_file": true,
	// Kustomize, built directly: the path is a policy-document resource path.
	"kustomize_policy_document_resource": false,
	// Ansible, built directly: the path is the cleaned file path.
	"ansible_inventory":       false,
	"ansible_vars":            false,
	"ansible_task_entrypoint": false,
	"ansible_playbook":        false,
	"ansible_role":            false,
}

// NameScoped reports whether the repository name is stripped from the path of
// rows of this evidence kind. known is false for a kind no extractor emits;
// a caller must not guess a scope for it.
func NameScoped(evidenceKind string) (scoped, known bool) {
	scoped, known = nameScopedByEvidenceKind[evidenceKind]
	return scoped, known
}

// EvidenceKinds returns every evidence_kind an extractor emits, sorted.
func EvidenceKinds() []string {
	kinds := make([]string, 0, len(nameScopedByEvidenceKind))
	for kind := range nameScopedByEvidenceKind {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
