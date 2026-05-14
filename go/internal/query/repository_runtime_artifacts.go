package query

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// buildRepositoryRuntimeArtifacts derives compact runtime-artifact summaries
// from repo-local runtime assets. It stays on the read side and only surfaces
// parser-proven local runtime artifacts.
func buildRepositoryRuntimeArtifacts(files []FileContent) map[string]any {
	artifacts := make([]map[string]any, 0)
	for _, file := range files {
		switch {
		case isDockerComposeArtifact(file):
			services := parseDockerComposeRuntimeArtifacts(file.Content)
			if len(services) == 0 {
				continue
			}

			for _, service := range services {
				row := map[string]any{
					"relative_path": file.RelativePath,
					"artifact_type": composeArtifactType(file),
					"service_name":  service.ServiceName,
					"signals":       service.signals(),
				}
				if service.BuildContext != "" {
					row["build_context"] = service.BuildContext
				}
				if command := composeRuntimeValues(service.Command); len(command) > 0 {
					row["command"] = command
				}
				if entrypoint := composeRuntimeValues(service.Entrypoint); len(entrypoint) > 0 {
					row["entrypoint"] = entrypoint
				}
				if ports := composeRuntimeValues(service.Ports); len(ports) > 0 {
					row["ports"] = ports
				}
				if environment := composeRuntimeValues(service.Environment); len(environment) > 0 {
					row["environment"] = environment
				}
				if envFiles := composeRuntimeValues(service.EnvFiles); len(envFiles) > 0 {
					row["env_files"] = envFiles
				}
				if configs := composeRuntimeValues(service.Configs); len(configs) > 0 {
					row["configs"] = configs
				}
				if secrets := composeRuntimeValues(service.Secrets); len(secrets) > 0 {
					row["secrets"] = secrets
				}
				if volumes := composeRuntimeValues(service.Volumes); len(volumes) > 0 {
					row["volumes"] = volumes
				}
				artifacts = append(artifacts, row)
			}
		case isDockerfileArtifact(file):
			artifacts = append(artifacts, buildDockerfileRuntimeArtifacts(file)...)
		}
	}

	if len(artifacts) == 0 {
		return nil
	}

	return map[string]any{
		"deployment_artifacts": artifacts,
	}
}

func loadRepositoryRuntimeArtifacts(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	files []FileContent,
) (map[string]any, error) {
	if reader == nil || repoID == "" {
		return nil, nil
	}

	candidates := files
	if candidates == nil {
		var err error
		candidates, err = reader.ListRepoFiles(ctx, repoID, repositorySemanticEntityLimit)
		if err != nil {
			return nil, fmt.Errorf("list runtime artifact files: %w", err)
		}
	}

	hydratedCandidates, err := hydrateRepositoryCandidateFiles(ctx, reader, repoID, candidates, func(file FileContent) bool {
		return isDockerComposeArtifact(file) || isDockerfileArtifact(file)
	})
	if err != nil {
		return nil, fmt.Errorf("hydrate runtime artifact files: %w", err)
	}

	contentFiles := make([]FileContent, 0, len(hydratedCandidates))
	for _, file := range hydratedCandidates {
		if !isDockerComposeArtifact(file) && !isDockerfileArtifact(file) {
			continue
		}
		if strings.TrimSpace(file.Content) == "" {
			continue
		}
		contentFiles = append(contentFiles, file)
	}

	return buildRepositoryRuntimeArtifacts(contentFiles), nil
}

func mergeDeploymentArtifactMaps(left map[string]any, right map[string]any) map[string]any {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}

	merged := map[string]any{}
	for key, value := range left {
		merged[key] = value
	}
	for key, value := range right {
		if existingRows, ok := mapSliceValue(merged, key), true; ok && len(existingRows) > 0 {
			incomingRows := mapSliceValue(right, key)
			if len(incomingRows) > 0 {
				merged[key] = append(existingRows, incomingRows...)
				continue
			}
		}
		merged[key] = value
	}
	return merged
}

type composeRuntimeArtifact struct {
	ServiceName  string
	BuildContext string
	Command      []string
	Entrypoint   []string
	Healthcheck  bool
	Ports        []string
	Environment  []string
	EnvFiles     []string
	Configs      []string
	Secrets      []string
	Volumes      []string
}

func (a composeRuntimeArtifact) signals() []string {
	signals := make([]string, 0, 8)
	if a.BuildContext != "" {
		signals = append(signals, "build")
	}
	if a.Healthcheck {
		signals = append(signals, "healthcheck")
	}
	if len(a.Ports) > 0 {
		signals = append(signals, "ports")
	}
	if len(a.Environment) > 0 {
		signals = append(signals, "environment")
	}
	if len(a.EnvFiles) > 0 {
		signals = append(signals, "env_files")
	}
	if len(a.Configs) > 0 {
		signals = append(signals, "configs")
	}
	if len(a.Secrets) > 0 {
		signals = append(signals, "secrets")
	}
	if len(a.Volumes) > 0 {
		signals = append(signals, "volumes")
	}
	return signals
}

func parseDockerComposeRuntimeArtifacts(content string) []composeRuntimeArtifact {
	lines := strings.Split(content, "\n")
	artifacts := make([]composeRuntimeArtifact, 0)

	var (
		current              *composeRuntimeArtifact
		inServices           bool
		servicesIndent       = -1
		currentSection       string
		currentSectionIndent int
	)

	flushCurrent := func() {
		if current == nil {
			return
		}
		current.Ports = composeRuntimeValues(current.Ports)
		current.Environment = composeRuntimeValues(current.Environment)
		current.EnvFiles = composeRuntimeValues(current.EnvFiles)
		current.Configs = composeRuntimeValues(current.Configs)
		current.Secrets = composeRuntimeValues(current.Secrets)
		current.Volumes = composeRuntimeValues(current.Volumes)
		current.Command = composeRuntimeValues(current.Command)
		current.Entrypoint = composeRuntimeValues(current.Entrypoint)
		if current.BuildContext != "" || len(current.Command) > 0 || len(current.Entrypoint) > 0 || current.Healthcheck || len(current.Ports) > 0 || len(current.Environment) > 0 || len(current.EnvFiles) > 0 || len(current.Configs) > 0 || len(current.Secrets) > 0 || len(current.Volumes) > 0 {
			artifacts = append(artifacts, *current)
		}
		current = nil
		currentSection = ""
		currentSectionIndent = 0
	}

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		indent := leadingWhitespaceWidth(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !inServices {
			if trimmed == "services:" {
				inServices = true
				servicesIndent = indent
			}
			continue
		}

		if indent <= servicesIndent {
			flushCurrent()
			inServices = false
			continue
		}

		for {
			if currentSection != "" && indent <= currentSectionIndent {
				currentSection = ""
				currentSectionIndent = 0
				continue
			}

			if current == nil {
				if serviceName, ok := composeServiceName(trimmed, indent, servicesIndent); ok {
					current = &composeRuntimeArtifact{ServiceName: serviceName}
				}
				break
			}

			if serviceName, ok := composeServiceName(trimmed, indent, servicesIndent); ok {
				flushCurrent()
				current = &composeRuntimeArtifact{ServiceName: serviceName}
				break
			}

			key, value, ok := yamlKeyValue(trimmed)
			if !ok {
				if currentSection == "" {
					break
				}
				composeCaptureRuntimeSectionValue(current, currentSection, trimmed)
				break
			}

			switch key {
			case "healthcheck":
				current.Healthcheck = true
			case "build", "ports", "environment", "env_file", "configs", "secrets", "volumes", "command", "entrypoint":
				currentSection = key
				currentSectionIndent = indent
				composeCaptureRuntimeSectionInlineValue(current, key, value)
			default:
				if currentSection == "" {
					break
				}
				composeCaptureRuntimeSectionValue(current, currentSection, trimmed)
			}
			break
		}
	}

	flushCurrent()
	return artifacts
}

func composeServiceName(trimmedLine string, indent, servicesIndent int) (string, bool) {
	if indent != servicesIndent+2 {
		return "", false
	}
	if !strings.HasSuffix(trimmedLine, ":") {
		return "", false
	}

	key := strings.TrimSuffix(trimmedLine, ":")
	if key == "" || composeKnownSectionKey(key) {
		return "", false
	}
	if strings.HasPrefix(key, "x-") {
		return "", false
	}
	return key, true
}

func composeKnownSectionKey(key string) bool {
	switch key {
	case "build", "cap_add", "cap_drop", "command", "container_name", "entrypoint",
		"configs", "depends_on", "environment", "env_file", "expose", "healthcheck",
		"image", "labels", "networks", "ports", "profiles", "restart",
		"secrets", "stdin_open", "tty", "user", "volumes", "working_dir":
		return true
	default:
		return false
	}
}

func composeCaptureRuntimeSectionInlineValue(current *composeRuntimeArtifact, section, value string) {
	if current == nil || value == "" {
		return
	}
	for _, item := range composeInlineValues(value) {
		composeCaptureRuntimeSectionValue(current, section, item)
	}
}

func composeCaptureRuntimeSectionValue(current *composeRuntimeArtifact, section, value string) {
	if current == nil || value == "" {
		return
	}

	normalized := strings.TrimSpace(value)
	if strings.HasPrefix(normalized, "- ") {
		normalized = strings.TrimSpace(strings.TrimPrefix(normalized, "- "))
	}

	switch section {
	case "build":
		if key, value, ok := yamlKeyValue(normalized); ok && key == "context" {
			current.BuildContext = composeNormalizeScalar(value)
			return
		}
		if current.BuildContext == "" {
			current.BuildContext = composeNormalizeScalar(normalized)
		}
	case "command":
		current.Command = append(current.Command, composeNormalizeScalar(normalized))
	case "entrypoint":
		current.Entrypoint = append(current.Entrypoint, composeNormalizeScalar(normalized))
	case "ports":
		current.Ports = append(current.Ports, composeNormalizeScalar(normalized))
	case "environment":
		if key, _, ok := yamlKeyValue(normalized); ok {
			current.Environment = append(current.Environment, key)
			return
		}
		current.Environment = append(current.Environment, composeEnvironmentName(normalized))
	case "env_file":
		if key, value, ok := yamlKeyValue(normalized); ok {
			if key == "path" || key == "file" {
				current.EnvFiles = append(current.EnvFiles, composeNormalizeScalar(value))
			}
			return
		}
		current.EnvFiles = append(current.EnvFiles, composeNormalizeScalar(normalized))
	case "configs":
		if key, value, ok := yamlKeyValue(normalized); ok {
			if key == "source" || key == "file" {
				current.Configs = append(current.Configs, composeNormalizeScalar(value))
			}
			return
		}
		current.Configs = append(current.Configs, composeNormalizeScalar(normalized))
	case "secrets":
		if key, value, ok := yamlKeyValue(normalized); ok {
			if key == "source" || key == "file" {
				current.Secrets = append(current.Secrets, composeNormalizeScalar(value))
			}
			return
		}
		current.Secrets = append(current.Secrets, composeNormalizeScalar(normalized))
	case "volumes":
		current.Volumes = append(current.Volumes, composeNormalizeScalar(normalized))
	}
}

func composeInlineValues(value string) []string {
	trimmed := composeNormalizeScalar(value)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		if inner == "" {
			return nil
		}
		parts := strings.Split(inner, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			if item := composeNormalizeScalar(part); item != "" {
				values = append(values, item)
			}
		}
		return values
	}
	return []string{trimmed}
}

func composeEnvironmentName(value string) string {
	trimmed := composeNormalizeScalar(value)
	if trimmed == "" {
		return ""
	}
	if before, _, ok := strings.Cut(trimmed, "="); ok {
		return strings.TrimSpace(before)
	}
	if before, _, ok := strings.Cut(trimmed, ":"); ok {
		return strings.TrimSpace(before)
	}
	return trimmed
}

func composeNormalizeScalar(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 {
		if (strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) ||
			(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
			trimmed = trimmed[1 : len(trimmed)-1]
		}
	}
	return strings.TrimSpace(trimmed)
}

func composeRuntimeValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func isDockerComposeArtifact(file FileContent) bool {
	if strings.EqualFold(file.ArtifactType, "docker_compose") {
		return true
	}

	return isDockerComposeFilename(strings.ToLower(filepath.Base(file.RelativePath)))
}

func composeArtifactType(file FileContent) string {
	if file.ArtifactType != "" {
		return file.ArtifactType
	}
	return "docker_compose"
}

func yamlKeyValue(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "- ") {
		return "", "", false
	}

	key, value, ok := strings.Cut(trimmed, ":")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func isDockerComposeFilename(name string) bool {
	return name == "compose.yaml" ||
		name == "compose.yml" ||
		name == "docker-compose.yaml" ||
		name == "docker-compose.yml" ||
		(strings.HasPrefix(name, "docker-compose.") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")))
}
