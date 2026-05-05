package relationships

import (
	"path/filepath"
	"strings"
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

func parseGitHubRefParts(raw string) (repo string, path string, version string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		version = strings.TrimSpace(trimmed[at+1:])
		trimmed = strings.TrimSpace(trimmed[:at])
	}
	trimmed = strings.TrimPrefix(trimmed, "./")
	trimmed = strings.TrimPrefix(trimmed, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		return "", trimmed, version
	}
	if parts[0] == ".github" {
		return "", trimmed, version
	}
	repo = strings.Join(parts[:2], "/")
	if len(parts) > 2 {
		path = strings.Join(parts[2:], "/")
	}
	return repo, path, version
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
