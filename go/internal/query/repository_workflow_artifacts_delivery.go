package query

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

var workflowDeliveryFamilyPatterns = []struct {
	family string
	regex  *regexp.Regexp
}{
	{family: "terraform", regex: regexp.MustCompile(`(^|[;&|]\s*)terraform(\s|$)`)},
	{family: "terragrunt", regex: regexp.MustCompile(`(^|[;&|]\s*)terragrunt(\s|$)`)},
	{family: "helm", regex: regexp.MustCompile(`(^|[;&|]\s*)helm(\s|$)`)},
	{family: "kubectl", regex: regexp.MustCompile(`(^|[;&|]\s*)kubectl(\s|$)`)},
	{family: "ansible", regex: regexp.MustCompile(`(^|[;&|]\s*)ansible-playbook(\s|$)`)},
	{family: "argocd", regex: regexp.MustCompile(`(^|[;&|]\s*)argocd(\s|$)`)},
	{family: "docker_compose", regex: regexp.MustCompile(`(^|[;&|]\s*)(docker\s+compose|docker-compose)(\s|$)`)},
}

func workflowDeliveryCommandFamilies(runCommands []string) []string {
	families := make([]string, 0, len(workflowDeliveryFamilyPatterns)+1)
	seen := make(map[string]struct{}, len(workflowDeliveryFamilyPatterns)+1)

	for _, command := range runCommands {
		normalized := normalizeWorkflowCommand(command)
		if normalized == "" || strings.HasPrefix(normalized, "echo ") {
			continue
		}
		for _, pattern := range workflowDeliveryFamilyPatterns {
			if !pattern.regex.MatchString(normalized) {
				continue
			}
			if _, exists := seen[pattern.family]; exists {
				continue
			}
			seen[pattern.family] = struct{}{}
			families = append(families, pattern.family)
		}
		if strings.Contains(normalized, "docker build") ||
			strings.Contains(normalized, "docker run") ||
			strings.Contains(normalized, "docker push") ||
			strings.Contains(normalized, "docker pull") ||
			strings.Contains(normalized, "docker login") ||
			strings.Contains(normalized, "docker tag") {
			if _, exists := seen["docker"]; !exists {
				seen["docker"] = struct{}{}
				families = append(families, "docker")
			}
		}
	}

	if len(families) == 0 {
		return nil
	}
	sort.Strings(families)
	return families
}

// workflowDeliveryLocalPaths extracts repo-local path hints from workflow run
// commands for read-side delivery summaries. These hints stay non-canonical
// until stronger repo-bearing evidence proves a relationship edge.
func workflowDeliveryLocalPaths(runCommands []string) []string {
	paths := make([]string, 0)
	seen := make(map[string]struct{})

	for _, command := range runCommands {
		for _, segment := range splitWorkflowCommandSegments(command) {
			fields := workflowCommandFields(segment)
			if len(fields) == 0 {
				continue
			}
			paths = appendWorkflowLocalPaths(paths, seen, extractTerraformLocalPaths(fields))
			paths = appendWorkflowLocalPaths(paths, seen, extractTerragruntLocalPaths(fields))
			paths = appendWorkflowLocalPaths(paths, seen, extractHelmLocalPaths(fields))
			paths = appendWorkflowLocalPaths(paths, seen, extractKubectlLocalPaths(fields))
			paths = appendWorkflowLocalPaths(paths, seen, extractAnsibleLocalPaths(fields))
			paths = appendWorkflowLocalPaths(paths, seen, extractDockerComposeLocalPaths(fields))
			paths = appendWorkflowLocalPaths(paths, seen, extractDockerBuildLocalPaths(fields))
		}
	}

	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	return paths
}

func appendWorkflowLocalPaths(paths []string, seen map[string]struct{}, candidates []string) []string {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		paths = append(paths, candidate)
	}
	return paths
}

func splitWorkflowCommandSegments(command string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	parts := strings.Split(replacer.Replace(command), "\n")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		segments = append(segments, trimmed)
	}
	return segments
}

func workflowCommandFields(command string) []string {
	command = strings.ReplaceAll(command, "\n", " ")
	command = strings.ReplaceAll(command, "\t", " ")
	return strings.Fields(command)
}

func extractTerraformLocalPaths(fields []string) []string {
	paths := make([]string, 0, 1)
	for index, field := range fields {
		if strings.HasPrefix(field, "-chdir=") || strings.HasPrefix(field, "--chdir=") {
			if cleaned := normalizeWorkflowLocalPathCandidate(strings.SplitN(field, "=", 2)[1]); cleaned != "" {
				paths = append(paths, cleaned)
			}
			continue
		}
		if field != "-chdir" && field != "--chdir" {
			continue
		}
		if index+1 >= len(fields) {
			continue
		}
		if cleaned := normalizeWorkflowLocalPathCandidate(fields[index+1]); cleaned != "" {
			paths = append(paths, cleaned)
		}
	}
	return paths
}

func extractTerragruntLocalPaths(fields []string) []string {
	paths := make([]string, 0, 1)
	for index, field := range fields {
		if strings.HasPrefix(field, "--terragrunt-working-dir=") {
			if cleaned := normalizeWorkflowLocalPathCandidate(strings.SplitN(field, "=", 2)[1]); cleaned != "" {
				paths = append(paths, cleaned)
			}
			continue
		}
		if field != "--terragrunt-working-dir" {
			continue
		}
		if index+1 >= len(fields) {
			continue
		}
		if cleaned := normalizeWorkflowLocalPathCandidate(fields[index+1]); cleaned != "" {
			paths = append(paths, cleaned)
		}
	}
	return paths
}

func extractHelmLocalPaths(fields []string) []string {
	if len(fields) < 2 || fields[0] != "helm" {
		return nil
	}
	candidate := normalizeWorkflowLocalPathCandidate(fields[len(fields)-1])
	if candidate == "" || !workflowLikelyLocalHelmPath(candidate) {
		return nil
	}
	return []string{candidate}
}

func extractKubectlLocalPaths(fields []string) []string {
	paths := make([]string, 0, 1)
	for index, field := range fields {
		switch {
		case field == "-f" || field == "--filename":
			if index+1 < len(fields) {
				if cleaned := normalizeWorkflowLocalPathCandidate(fields[index+1]); cleaned != "" {
					paths = append(paths, cleaned)
				}
			}
		case strings.HasPrefix(field, "--filename="):
			if cleaned := normalizeWorkflowLocalPathCandidate(strings.SplitN(field, "=", 2)[1]); cleaned != "" {
				paths = append(paths, cleaned)
			}
		}
	}
	return paths
}

func extractAnsibleLocalPaths(fields []string) []string {
	if len(fields) == 0 || fields[0] != "ansible-playbook" {
		return nil
	}

	paths := make([]string, 0, 3)
	playbookRecorded := false
	for index := 1; index < len(fields); index++ {
		field := fields[index]
		if strings.HasPrefix(field, "-") {
			switch {
			case field == "-i" || field == "--inventory":
				if index+1 < len(fields) {
					if cleaned := normalizeWorkflowLocalPathCandidate(fields[index+1]); cleaned != "" {
						paths = append(paths, cleaned)
					}
					index++
				}
			case strings.HasPrefix(field, "--inventory="):
				if cleaned := normalizeWorkflowLocalPathCandidate(strings.SplitN(field, "=", 2)[1]); cleaned != "" {
					paths = append(paths, cleaned)
				}
			case field == "-e" || field == "--extra-vars":
				if index+1 < len(fields) {
					if cleaned := normalizeWorkflowExtraVarsPath(fields[index+1]); cleaned != "" {
						paths = append(paths, cleaned)
					}
					index++
				}
			case strings.HasPrefix(field, "--extra-vars="):
				if cleaned := normalizeWorkflowExtraVarsPath(strings.SplitN(field, "=", 2)[1]); cleaned != "" {
					paths = append(paths, cleaned)
				}
			}
			continue
		}
		if !playbookRecorded {
			if cleaned := normalizeWorkflowLocalPathCandidate(field); cleaned != "" {
				paths = append(paths, cleaned)
				playbookRecorded = true
			}
			continue
		}
		if strings.HasPrefix(field, "@") {
			if cleaned := normalizeWorkflowExtraVarsPath(field); cleaned != "" {
				paths = append(paths, cleaned)
			}
			continue
		}
		if cleaned := normalizeWorkflowLocalPathCandidate(field); cleaned != "" {
			paths = append(paths, cleaned)
		}
	}
	return paths
}

func extractDockerComposeLocalPaths(fields []string) []string {
	if len(fields) < 2 {
		return nil
	}
	isCompose := fields[0] == "docker-compose" || (fields[0] == "docker" && fields[1] == "compose")
	if !isCompose {
		return nil
	}

	paths := make([]string, 0, 1)
	for index, field := range fields {
		switch {
		case field == "-f" || field == "--file":
			if index+1 < len(fields) {
				if cleaned := normalizeWorkflowLocalPathCandidate(fields[index+1]); cleaned != "" {
					paths = append(paths, cleaned)
				}
			}
		case strings.HasPrefix(field, "--file="):
			if cleaned := normalizeWorkflowLocalPathCandidate(strings.SplitN(field, "=", 2)[1]); cleaned != "" {
				paths = append(paths, cleaned)
			}
		}
	}
	return paths
}

func extractDockerBuildLocalPaths(fields []string) []string {
	if len(fields) < 2 || fields[0] != "docker" {
		return nil
	}
	if fields[1] != "build" && (fields[1] != "buildx" || len(fields) <= 2 || fields[2] != "build") {
		return nil
	}
	candidate := normalizeWorkflowLocalPathCandidate(fields[len(fields)-1])
	if candidate == "" {
		return nil
	}
	return []string{candidate}
}

func normalizeWorkflowExtraVarsPath(candidate string) string {
	candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "@")
	return normalizeWorkflowLocalPathCandidate(candidate)
}

func normalizeWorkflowLocalPathCandidate(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	candidate = strings.Trim(candidate, `"'`)
	if candidate == "" || candidate == "<nil>" {
		return ""
	}
	switch {
	case strings.HasPrefix(candidate, "/"),
		strings.Contains(candidate, "://"),
		strings.Contains(candidate, "${"),
		strings.HasPrefix(candidate, "-"),
		strings.Contains(candidate, ","),
		strings.Contains(candidate, "@"):
		return ""
	case candidate == ".":
		return "."
	default:
		return cleanRepositoryRelativePath(candidate)
	}
}

func workflowLikelyLocalHelmPath(candidate string) bool {
	if candidate == "." || strings.HasPrefix(candidate, "..") {
		return true
	}
	switch path.Clean(strings.Split(candidate, "/")[0]) {
	case "charts", "chart", "helm", "deploy", "k8s", "manifests", "infra", "terraform":
		return true
	default:
		return false
	}
}

func normalizeWorkflowCommand(command string) string {
	normalized := strings.ToLower(strings.TrimSpace(command))
	normalized = strings.ReplaceAll(normalized, "\n", " ")
	normalized = strings.ReplaceAll(normalized, "\t", " ")
	return strings.Join(strings.Fields(normalized), " ")
}
