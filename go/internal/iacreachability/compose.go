package iacreachability

import (
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type composeDocument struct {
	Services map[string]any `yaml:"services"`
}

func composeServiceArtifacts(file File) []artifact {
	if !isComposeFile(file.RelativePath) {
		return nil
	}
	var doc composeDocument
	if err := yaml.Unmarshal([]byte(file.Content), &doc); err != nil || len(doc.Services) == 0 {
		return nil
	}
	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	artifacts := make([]artifact, 0, len(names))
	for _, name := range names {
		artifacts = append(artifacts, artifact{
			family: "compose",
			repoID: file.RepoID,
			path:   "services/" + name,
			name:   name,
			evidence: []string{
				file.RelativePath + ": compose service exists",
			},
		})
	}
	return artifacts
}

func isComposeFile(relativePath string) bool {
	switch strings.ToLower(path.Base(relativePath)) {
	case "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":
		return true
	default:
		return false
	}
}

func recordComposeReferences(index referenceIndex, file File) {
	for _, line := range strings.Split(file.Content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "docker compose") && !strings.Contains(line, "docker-compose") {
			continue
		}
		evidence := file.RelativePath + ": compose service reference " + line
		if strings.Contains(line, "{{") || strings.Contains(line, "${") {
			for _, token := range referenceTokens(file.Content) {
				addReference(index.ambiguous, "compose", token, evidence)
			}
			continue
		}
		for _, service := range composeServicesFromCommand(line) {
			addReference(index.used, "compose", service, evidence)
		}
	}
}

func composeServicesFromCommand(line string) []string {
	fields := strings.Fields(strings.NewReplacer("'", " ", `"`, " ").Replace(line))
	commands := map[string]bool{
		"exec":    true,
		"logs":    true,
		"restart": true,
		"run":     true,
		"start":   true,
		"stop":    true,
		"up":      true,
	}
	seenCommand := false
	var services []string
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if !seenCommand {
			if commands[field] {
				seenCommand = true
			}
			continue
		}
		if strings.HasPrefix(field, "-") || field == "docker" || field == "compose" || field == "docker-compose" {
			continue
		}
		if strings.Contains(field, "/") || strings.Contains(field, ".") || strings.Contains(field, "=") {
			continue
		}
		services = append(services, field)
	}
	return services
}
