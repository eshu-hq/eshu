// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/environment"
	"github.com/eshu-hq/eshu/go/internal/ghactionsref"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// resolvedRelationshipEvidenceArtifacts returns bounded graph-story summaries
// from the resolver preview. Raw evidence details remain in Postgres.
// Citation fields (start_line, end_line, commit_sha) are projected from the
// evidence preview details when present so the graph EvidenceArtifact node
// carries byte-level provenance for the query surface. For GITHUB_ACTIONS_*
// evidence kinds only, ref_value/ref_pinned (issue #5372) project the
// action/workflow @ref and whether it is a full-length commit SHA, using the
// same ghactionsref.Pinned classifier the query-package read-model path uses
// (go/internal/query/repository_deployment_evidence_read_model.go), so the
// graph-projection path and the read-model path agree.
func resolvedRelationshipEvidenceArtifacts(r relationships.ResolvedRelationship) []map[string]any {
	items := evidencePreviewItems(r.Details["evidence_preview"])
	if len(items) == 0 {
		return nil
	}

	artifacts := make([]map[string]any, 0, len(items))
	for _, item := range items {
		kind := strings.TrimSpace(anyString(item["kind"]))
		details := artifactDetails(item["details"])
		path := firstArtifactString(details, "path", "first_party_ref_path", "config_path", "file_path")
		matchedValue := firstArtifactString(details, "matched_value", "first_party_ref_normalized", "source_ref", "image_ref")
		matchedAlias := firstArtifactString(details, "matched_alias", "first_party_ref_name", "flux_git_repository_name", "name")
		if kind == string(relationships.EvidenceKindFluxGitRepositorySource) {
			matchedValue = firstArtifactString(details, "normalized_url", "url", "matched_value")
		}
		if kind == "" || (path == "" && matchedValue == "" && matchedAlias == "") {
			continue
		}
		artifact := map[string]any{
			"evidence_kind":   kind,
			"artifact_family": artifactFamily(kind),
			"path":            path,
			"extractor":       firstArtifactString(details, "extractor", "parser", "source"),
			"environment":     environmentFromArtifactPath(path),
			"matched_alias":   matchedAlias,
			"matched_value":   matchedValue,
			"confidence":      artifactConfidence(item["confidence"]),
		}
		if kind == string(relationships.EvidenceKindFluxGitRepositorySource) {
			artifact["flux_git_repository_name"] = firstArtifactString(details, "flux_git_repository_name")
			artifact["flux_git_repository_namespace"] = firstArtifactString(details, "flux_git_repository_namespace")
		}
		if runtimeKind := firstArtifactString(details, "runtime_platform_kind", "platform_kind"); runtimeKind != "" {
			artifact["runtime_platform_kind"] = runtimeKind
		}
		// Propagate byte-level citation fields when available. Absent fields are
		// omitted so the graph node does not store zero-valued noise.
		if sl := artifactPositiveInt(details, "start_line"); sl > 0 {
			artifact["start_line"] = sl
		}
		if el := artifactPositiveInt(details, "end_line"); el > 0 {
			artifact["end_line"] = el
		}
		if sha := firstArtifactString(details, "commit_sha"); sha != "" {
			artifact["commit_sha"] = sha
		}
		// GitHub Actions @ref pin signal (issue #5372), scoped strictly to
		// GITHUB_ACTIONS_* evidence kinds: first_party_ref_version is also
		// populated by unrelated evidence families (Terraform module
		// versions, Ansible role refs, Chef cookbook versions, ...) via the
		// shared withFirstPartyRefDetails helper, and attaching a GitHub
		// Actions pin-safety label to one of those would fabricate a claim
		// the evidence never made. ref_pinned is derived with
		// ghactionsref.Pinned -- the single classifier the query-package
		// read-model path (deploymentEvidenceArtifactFromPreview) also uses,
		// so the two paths cannot disagree. Both fields are omitted together
		// when no ref exists (local ./ workflow, docker action): never
		// default ref_pinned to true for a workflow with no ref.
		if strings.HasPrefix(kind, "GITHUB_ACTIONS_") {
			if refValue := firstArtifactString(details, "first_party_ref_version", "action_ref_name", "workflow_ref_name"); refValue != "" {
				artifact["ref_value"] = refValue
				artifact["ref_pinned"] = ghactionsref.Pinned(refValue)
			}
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts
}

// artifactPositiveInt reads an integer-valued key from a details map and
// returns it only when it is > 0. JSON round-tripping stores numbers as
// float64 so both int and float64 are accepted.
func artifactPositiveInt(details map[string]any, key string) int {
	if details == nil {
		return 0
	}
	switch typed := details[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func evidencePreviewItems(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		items := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				items = append(items, mapped)
			}
		}
		return items
	default:
		return nil
	}
}

func artifactDetails(value any) map[string]any {
	details, _ := value.(map[string]any)
	return details
}

func firstArtifactString(details map[string]any, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(details[key]))
		if value == "" || value == "<nil>" {
			continue
		}
		return value
	}
	return ""
}

func artifactConfidence(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func artifactFamily(kind string) string {
	switch {
	case strings.HasPrefix(kind, "ARGOCD_"):
		return "argocd"
	case strings.HasPrefix(kind, "HELM_"):
		return "helm"
	case strings.HasPrefix(kind, "KUSTOMIZE_"):
		return "kustomize"
	case strings.HasPrefix(kind, "TERRAGRUNT_"):
		return "terragrunt"
	case strings.HasPrefix(kind, "TERRAFORM_"):
		return "terraform"
	case strings.HasPrefix(kind, "GITHUB_ACTIONS_"):
		return "github_actions"
	case strings.HasPrefix(kind, "JENKINS_"):
		return "jenkins"
	case strings.HasPrefix(kind, "ANSIBLE_"):
		return "ansible"
	case strings.HasPrefix(kind, "DOCKER_COMPOSE_"):
		return "docker_compose"
	case strings.HasPrefix(kind, "DOCKERFILE_"):
		return "dockerfile"
	default:
		return strings.ToLower(kind)
	}
}

func environmentFromArtifactPath(path string) string {
	for _, segment := range strings.Split(path, "/") {
		if segment == "" {
			continue
		}
		for _, token := range strings.FieldsFunc(segment, func(r rune) bool {
			return r == '-' || r == '_' || r == '.'
		}) {
			if isKnownEnvironmentToken(strings.ToLower(token)) {
				return segment
			}
		}
	}
	return ""
}

func isKnownEnvironmentToken(token string) bool {
	return environment.IsKnownToken(token)
}
