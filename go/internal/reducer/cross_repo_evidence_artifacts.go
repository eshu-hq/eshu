package reducer

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// resolvedRelationshipEvidenceArtifacts returns bounded graph-story summaries
// from the resolver preview. Raw evidence details remain in Postgres.
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
		matchedAlias := firstArtifactString(details, "matched_alias", "first_party_ref_name", "name")
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
		if runtimeKind := firstArtifactString(details, "runtime_platform_kind", "platform_kind"); runtimeKind != "" {
			artifact["runtime_platform_kind"] = runtimeKind
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts
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
	switch token {
	case "dev", "development", "test", "qa", "stage", "staging", "uat", "preprod", "prod", "production", "sandbox", "preview":
		return true
	default:
		return false
	}
}
