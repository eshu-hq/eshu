// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ghactionsref"
)

func withFirstPartyRefDetails(
	details map[string]any,
	kind, name, path, root, version, normalized string,
) map[string]any {
	if details == nil {
		details = map[string]any{}
	}
	if kind != "" {
		details["first_party_ref_kind"] = kind
	}
	if name != "" {
		details["first_party_ref_name"] = name
	}
	if path != "" {
		details["first_party_ref_path"] = path
	}
	if root != "" {
		details["first_party_ref_root"] = root
	}
	if version != "" {
		details["first_party_ref_version"] = version
	}
	if normalized != "" {
		details["first_party_ref_normalized"] = normalized
	}
	return details
}

func mergeDetails(base map[string]any, extras ...map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range base {
		result[key] = value
	}
	for _, extra := range extras {
		for key, value := range extra {
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func csvValues(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parts := strings.Split(typed, ",")
		values := make([]string, 0, len(parts))
		seen := make(map[string]struct{}, len(parts))
		for _, part := range parts {
			candidate := strings.TrimSpace(part)
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			values = append(values, candidate)
		}
		return values
	default:
		return nil
	}
}

func normalizeHelmRefValue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.Trim(trimmed, `"`)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "file://") {
		return strings.TrimPrefix(trimmed, "file://")
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}
	lastSlash := strings.LastIndex(trimmed, "/")
	lastColon := strings.LastIndex(trimmed, ":")
	if lastColon > lastSlash {
		trimmed = trimmed[:lastColon]
	}
	return trimmed
}

// parseGitHubRefParts splits a GitHub Actions `uses:`/reusable-workflow
// reference into its repository slug, in-repo path, and @ref (version)
// components. It delegates to ghactionsref.Parse -- the single ref-splitting
// implementation issue #5372 introduced so this package's evidence
// extraction and the query package's read-model re-parsing cannot silently
// diverge. Behavior-preserving: the delegated logic is byte-identical to the
// implementation this function used to contain, so every existing Details key
// this package's callers populate (first_party_ref_path,
// first_party_ref_version, action_ref_name, workflow_ref_name, ...) is
// unchanged.
func parseGitHubRefParts(raw string) (repo string, path string, version string) {
	return ghactionsref.Parse(raw)
}

func normalizeRepositoryURLName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ".git")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "git+https://", "https://")
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 && idx < len(trimmed)-1 {
		return trimmed[idx+1:]
	}
	if idx := strings.LastIndex(trimmed, ":"); idx >= 0 && idx < len(trimmed)-1 {
		return trimmed[idx+1:]
	}
	return trimmed
}

func normalizeAnsibleReference(candidate ansibleRoleCandidate) (kind, name, normalized string) {
	raw := strings.TrimSpace(candidate.value)
	switch candidate.key {
	case "import_playbook":
		kind = "ansible_import_playbook"
		name = strings.TrimSuffix(filepath.Base(raw), filepath.Ext(raw))
		normalized = name
	default:
		kind = "ansible_role_source"
		name = strings.TrimSpace(candidate.roleName)
		if strings.Contains(raw, "github.com") || strings.Contains(raw, "git@") {
			normalized = normalizeRepositoryURLName(raw)
			if normalized != "" && name == "" {
				name = normalized
			}
			return kind, name, normalized
		}
		normalized = strings.TrimSuffix(filepath.Base(raw), filepath.Ext(raw))
		if normalized == "" {
			normalized = raw
		}
	}
	return kind, name, normalized
}

// ExtractTerraformRefPin reads the go-getter style `ref=` query parameter off
// a raw Terraform/Terragrunt module source string (e.g.
// "git::https://host/mod.git?ref=v1.2.3" -> "v1.2.3"). It is the counterpart
// to normalizeTerraformFirstPartyRef that recovers the pin value instead of
// stripping it; normalizeTerraformFirstPartyRef's behavior is intentionally
// left untouched (it has pinned consumers) since edges now carry the pin as
// its own first_party_ref_version property alongside the existing stripped
// first_party_ref_normalized value. Returns "" when the source has no query
// string, no ref parameter, or an empty ref value. A trailing URL fragment
// (everything from the first "#" onward) is stripped from the extracted
// value: a fragment identifier is never part of the ref itself, so
// "?ref=v1.2.3#subdir" yields "v1.2.3", not "v1.2.3#subdir" (P2 finding F7).
// Exported so evidenceFactFirstPartyRefVersion
// (evidence_edge_fields.go) can derive the first_party_ref_version edge
// property from EvidenceFact.Details["source_ref"] without duplicating the
// parsing rule.
func ExtractTerraformRefPin(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	idx := strings.Index(trimmed, "?")
	if idx < 0 {
		return ""
	}
	query := trimmed[idx+1:]
	for _, param := range strings.Split(query, "&") {
		key, value, ok := strings.Cut(param, "=")
		if !ok || key != "ref" {
			continue
		}
		if fragmentIdx := strings.Index(value, "#"); fragmentIdx >= 0 {
			value = value[:fragmentIdx]
		}
		return strings.TrimSpace(value)
	}
	return ""
}

func normalizeTerraformFirstPartyRef(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.Trim(trimmed, `"`)
	if trimmed == "" {
		return ""
	}
	if normalized := normalizeTerraformEvidencePathExpression(trimmed); normalized != "" {
		return normalized
	}
	if idx := strings.Index(trimmed, "?"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimPrefix(trimmed, "git::")
	if idx := strings.Index(trimmed, ".git//"); idx >= 0 {
		trimmed = trimmed[:idx+4]
	}
	return strings.TrimSpace(trimmed)
}
