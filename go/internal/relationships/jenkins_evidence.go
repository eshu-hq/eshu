package relationships

import (
	"regexp"
	"strings"
)

var jenkinsGitHubRepoPattern = regexp.MustCompile(`(?i)github\.com[:/][^/"'\s]+/([A-Za-z0-9._-]+)(?:\.git)?`)

func discoverJenkinsEvidence(
	sourceRepoID, filePath, content string,
	parsedFileData map[string]any,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	commonDetails := jenkinsEvidenceDetails(parsedFileData)

	for _, library := range jenkinsSharedLibraries(parsedFileData) {
		details := withFirstPartyRefDetails(
			cloneDetails(commonDetails),
			"jenkins_shared_library",
			library,
			"",
			"",
			"",
			library,
		)
		details["shared_library"] = library
		evidence = append(evidence, matchCatalog(
			sourceRepoID, library, filePath,
			EvidenceKindJenkinsSharedLibrary, RelDiscoversConfigIn, 0.89,
			"Jenkins shared library references configuration or automation in the target repository",
			"jenkins", catalog, seen, details,
		)...)
	}

	for _, repoRef := range jenkinsGitHubRepoRefs(content, parsedFileData) {
		repositoryName := normalizeRepositoryURLName(repoRef)
		details := withFirstPartyRefDetails(
			cloneDetails(commonDetails),
			"jenkins_repository",
			repositoryName,
			"",
			"",
			"",
			repositoryName,
		)
		details["repository_ref"] = repoRef
		details["repository_name"] = repositoryName
		evidence = append(evidence, matchCatalog(
			sourceRepoID, repoRef, filePath,
			EvidenceKindJenkinsGitHubRepository, RelDiscoversConfigIn, 0.92,
			"Jenkins pipeline references the target repository through an explicit GitHub URL",
			"jenkins", catalog, seen, details,
		)...)
	}

	return evidence
}

func jenkinsEvidenceDetails(parsedFileData map[string]any) map[string]any {
	if len(parsedFileData) == 0 {
		return nil
	}

	details := make(map[string]any)
	if pipelineCalls := payloadAnyStringSlice(parsedFileData["pipeline_calls"]); len(pipelineCalls) > 0 {
		details["pipeline_calls"] = pipelineCalls
	}
	if entryPoints := payloadAnyStringSlice(parsedFileData["entry_points"]); len(entryPoints) > 0 {
		details["entry_points"] = entryPoints
	}
	if shellCommands := payloadAnyStringSlice(parsedFileData["shell_commands"]); len(shellCommands) > 0 {
		details["shell_commands"] = shellCommands
	}
	if hints := payloadAnyMapSlice(parsedFileData["ansible_playbook_hints"]); len(hints) > 0 {
		details["ansible_playbook_hints"] = hints
	}
	if useConfigd, ok := parsedFileData["use_configd"].(bool); ok {
		details["use_configd"] = useConfigd
	}
	if hasPreDeploy, ok := parsedFileData["has_pre_deploy"].(bool); ok {
		details["has_pre_deploy"] = hasPreDeploy
	}
	if len(details) == 0 {
		return nil
	}
	return details
}

func cloneDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(details))
	for key, value := range details {
		cloned[key] = value
	}
	return cloned
}

func jenkinsSharedLibraries(parsedFileData map[string]any) []string {
	if len(parsedFileData) == 0 {
		return nil
	}
	values := payloadAnyStringSlice(parsedFileData["shared_libraries"])
	libs := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if at := strings.Index(value, "@"); at >= 0 {
			value = value[:at]
		}
		if value == "" {
			continue
		}
		libs = append(libs, value)
	}
	return uniqueStrings(libs)
}

func jenkinsGitHubRepoRefs(content string, parsedFileData map[string]any) []string {
	candidates := make([]string, 0)
	for _, match := range jenkinsGitHubRepoPattern.FindAllStringSubmatch(content, -1) {
		if len(match) >= 2 {
			candidates = append(candidates, strings.TrimSpace(match[1]))
		}
	}

	for _, command := range payloadAnyStringSlice(parsedFileData["shell_commands"]) {
		for _, match := range jenkinsGitHubRepoPattern.FindAllStringSubmatch(command, -1) {
			if len(match) >= 2 {
				candidates = append(candidates, strings.TrimSpace(match[1]))
			}
		}
	}

	return uniqueStrings(candidates)
}

func payloadAnyStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func payloadAnyMapSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			entry, ok := item.(map[string]any)
			if ok {
				result = append(result, entry)
			}
		}
		return result
	default:
		return nil
	}
}
