package query

import "strings"

type githubActionsRelationship struct {
	reason           string
	relationshipType string
	targetName       string
}

func githubActionsMetadataRelationships(metadata map[string]any) []githubActionsRelationship {
	relationships := make([]githubActionsRelationship, 0, 4)
	for _, workflowRef := range metadataStringSlice(metadata, "workflow_refs") {
		if targetName := githubActionsRepositoryRef(workflowRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetName,
				reason:           "github_actions_reusable_workflow_ref",
			})
			continue
		}
		if targetPath := githubActionsLocalReusableWorkflowPath(workflowRef); targetPath != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetPath,
				reason:           "github_actions_local_reusable_workflow_ref",
			})
		}
	}
	for _, workflowRef := range metadataStringSlice(metadata, "workflow_ref") {
		if targetName := githubActionsRepositoryRef(workflowRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetName,
				reason:           "github_actions_reusable_workflow_ref",
			})
			continue
		}
		if targetPath := githubActionsLocalReusableWorkflowPath(workflowRef); targetPath != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetPath,
				reason:           "github_actions_local_reusable_workflow_ref",
			})
		}
	}
	for _, repoRef := range metadataStringSlice(metadata, "checkout_repositories") {
		if targetName := githubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_checkout_repository",
			})
		}
	}
	for _, repoRef := range metadataStringSlice(metadata, "checkout_repository") {
		if targetName := githubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_checkout_repository",
			})
		}
	}
	for _, repoRef := range githubActionsWorkflowInputRepositoryMetadata(metadata) {
		if targetName := githubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_workflow_input_repository",
			})
		}
	}
	for _, repoRef := range metadataStringSlice(metadata, "action_repositories") {
		if targetName := githubActionsActionRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPENDS_ON",
				targetName:       targetName,
				reason:           "github_actions_action_repository",
			})
		}
	}
	return relationships
}

func githubActionsSourceRelationships(entity EntityContent) []githubActionsRelationship {
	if !entityLooksLikeGitHubActionsWorkflow(entity) {
		return nil
	}

	lines := strings.Split(entity.SourceCache, "\n")
	relationships := make([]githubActionsRelationship, 0, 2)
	for i := 0; i < len(lines); i++ {
		usesValue, ok := yamlScalarValue(lines[i], "uses")
		if !ok {
			continue
		}
		if targetName := githubActionsReusableWorkflowRepoRef(usesValue); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetName,
				reason:           "github_actions_reusable_workflow_ref",
			})
		} else if targetPath := githubActionsLocalReusableWorkflowPath(usesValue); targetPath != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetPath,
				reason:           "github_actions_local_reusable_workflow_ref",
			})
		}
		for j := i + 1; j < len(lines); j++ {
			nextTrimmed := strings.TrimSpace(lines[j])
			if nextTrimmed == "" || strings.HasPrefix(nextTrimmed, "#") {
				continue
			}
			if leadingWhitespaceWidth(lines[j]) < leadingWhitespaceWidth(lines[i]) {
				break
			}
			for _, key := range []string{"workflow_input_repository", "workflow_input_repositories", "automation-repo", "automation_repo"} {
				for _, repoValue := range yamlRepositoryValues(lines, j, key) {
					targetName := githubActionsRepositoryRef(repoValue)
					if targetName == "" {
						continue
					}
					relationships = append(relationships, githubActionsRelationship{
						relationshipType: "DISCOVERS_CONFIG_IN",
						targetName:       targetName,
						reason:           "github_actions_workflow_input_repository",
					})
				}
			}
		}
		if targetName := githubActionsReusableWorkflowRepoRef(usesValue); targetName != "" {
			continue
		}
		if targetName := githubActionsActionRepositoryRef(usesValue); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPENDS_ON",
				targetName:       targetName,
				reason:           "github_actions_action_repository",
			})
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(usesValue), "actions/checkout@") {
			continue
		}
		stepIndent := leadingWhitespaceWidth(lines[i])
		for j := i + 1; j < len(lines); j++ {
			nextTrimmed := strings.TrimSpace(lines[j])
			if nextTrimmed == "" || strings.HasPrefix(nextTrimmed, "#") {
				continue
			}
			if leadingWhitespaceWidth(lines[j]) <= stepIndent {
				break
			}
			repoValue, ok := yamlScalarValue(lines[j], "repository")
			if !ok {
				continue
			}
			if targetName := githubActionsRepositoryRef(repoValue); targetName != "" {
				relationships = append(relationships, githubActionsRelationship{
					relationshipType: "DISCOVERS_CONFIG_IN",
					targetName:       targetName,
					reason:           "github_actions_checkout_repository",
				})
			}
			break
		}
	}
	return relationships
}

func entityLooksLikeGitHubActionsWorkflow(entity EntityContent) bool {
	if strings.Contains(strings.ToLower(entity.RelativePath), ".github/workflows/") {
		return true
	}
	if strings.Contains(strings.ToLower(entity.SourceCache), "actions/checkout@") {
		return true
	}
	if strings.Contains(strings.ToLower(entity.SourceCache), "/.github/workflows/") {
		return true
	}
	return len(metadataStringSlice(entity.Metadata, "workflow_refs")) > 0 ||
		len(metadataStringSlice(entity.Metadata, "workflow_ref")) > 0 ||
		len(metadataStringSlice(entity.Metadata, "checkout_repositories")) > 0 ||
		len(metadataStringSlice(entity.Metadata, "checkout_repository")) > 0 ||
		len(metadataStringSlice(entity.Metadata, "action_repositories")) > 0 ||
		len(githubActionsWorkflowInputRepositoryMetadata(entity.Metadata)) > 0
}

func githubActionsWorkflowInputRepositoryMetadata(metadata map[string]any) []string {
	refs := make([]string, 0, 2)
	for _, key := range []string{"workflow_input_repository", "workflow_input_repositories", "automation-repo", "automation_repo"} {
		refs = append(refs, metadataStringSlice(metadata, key)...)
	}
	return refs
}

func githubActionsReusableWorkflowRepoRef(value string) string {
	trimmed := strings.TrimSpace(trimGitHubActionsScalar(value))
	if trimmed == "" {
		return ""
	}
	at := strings.Index(trimmed, "@")
	if at >= 0 {
		trimmed = trimmed[:at]
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return ""
	}
	if parts[0] == "." {
		return ""
	}
	if parts[2] != ".github" {
		return ""
	}
	return strings.Join(parts[:2], "/")
}

func githubActionsRepositoryRef(value string) string {
	trimmed := strings.TrimSpace(trimGitHubActionsScalar(value))
	if trimmed == "" {
		return ""
	}
	if repoRef := githubActionsReusableWorkflowRepoRef(trimmed); repoRef != "" {
		return repoRef
	}
	if isGitHubRepoSlug(trimmed) {
		return trimmed
	}
	return ""
}

func githubActionsActionRepositoryRef(value string) string {
	trimmed := strings.TrimSpace(trimGitHubActionsScalar(value))
	if trimmed == "" || strings.HasPrefix(trimmed, "docker://") {
		return ""
	}
	if strings.HasPrefix(trimmed, "actions/checkout@") {
		return ""
	}
	if repoRef := githubActionsReusableWorkflowRepoRef(trimmed); repoRef != "" {
		return ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}
	if strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, ".github/") {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[0] == "." {
		return ""
	}
	return strings.Join(parts[:2], "/")
}

func isGitHubRepoSlug(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return false
	}
	return parts[0] != "" && parts[1] != ""
}

func yamlScalarValue(line string, key string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "- ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	}
	prefix := key + ":"
	if !strings.HasPrefix(trimmed, prefix) {
		return "", false
	}
	value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	if value == "" {
		return "", false
	}
	return trimGitHubActionsScalar(value), true
}

func yamlRepositoryValues(lines []string, index int, key string) []string {
	if index < 0 || index >= len(lines) {
		return nil
	}
	if value, ok := yamlScalarValue(lines[index], key); ok {
		return []string{value}
	}

	trimmed := strings.TrimSpace(lines[index])
	prefix := key + ":"
	if !strings.HasPrefix(trimmed, prefix) {
		return nil
	}
	if strings.TrimSpace(strings.TrimPrefix(trimmed, prefix)) != "" {
		return nil
	}

	parentIndent := leadingWhitespaceWidth(lines[index])
	values := make([]string, 0, 2)
	for i := index + 1; i < len(lines); i++ {
		nextTrimmed := strings.TrimSpace(lines[i])
		if nextTrimmed == "" || strings.HasPrefix(nextTrimmed, "#") {
			continue
		}
		nextIndent := leadingWhitespaceWidth(lines[i])
		if nextIndent <= parentIndent {
			break
		}
		if !strings.HasPrefix(nextTrimmed, "- ") {
			break
		}
		value := trimGitHubActionsScalar(strings.TrimSpace(strings.TrimPrefix(nextTrimmed, "- ")))
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func trimGitHubActionsScalar(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 {
		return trimmed
	}
	if (strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) ||
		(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
		return trimmed[1 : len(trimmed)-1]
	}
	return trimmed
}

func leadingWhitespaceWidth(value string) int {
	return len(value) - len(strings.TrimLeft(value, " \t"))
}
