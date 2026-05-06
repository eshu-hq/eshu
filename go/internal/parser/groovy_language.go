package parser

import (
	"regexp"
	"slices"
	"strings"
)

var (
	groovyLibraryPattern      = regexp.MustCompile(`@Library\(['"]([^'"]+)['"]\)`)
	groovyLibraryStepPattern  = regexp.MustCompile(`(?is)\blibrary\s*(?:\(\s*)?(?:identifier\s*:\s*)?['"]([^'"]+)['"]`)
	groovyPipelineCallPattern = regexp.MustCompile(`\b(pipeline[A-Za-z0-9_]*)\s*\(`)
	groovyShellCommandPattern = regexp.MustCompile(`\bsh\s+['"]([^'"]+)['"]`)
	groovyAnsiblePattern      = regexp.MustCompile(`ansible-playbook\s+([^\s]+)(?:.*?-i\s+([^\s]+))?`)
	groovyEntryPointPattern   = regexp.MustCompile(`entry_point\s*:\s*['"]([^'"]+)['"]`)
	groovyUseConfigdPattern   = regexp.MustCompile(`use_configd\s*:\s*(true|false)`)
	groovyPreDeployPattern    = regexp.MustCompile(`pre_deploy\s*:`)
)

func (e *Engine) parseGroovy(path string, isDependency bool, options Options) (map[string]any, error) {
	sourceBytes, err := readSource(path)
	if err != nil {
		return nil, err
	}

	sourceText := string(sourceBytes)
	payload := basePayload(path, "groovy", isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}

	metadata := extractGroovyPipelineMetadata(sourceText)
	for key, value := range metadata {
		payload[key] = value
	}
	if options.IndexSource {
		payload["source"] = sourceText
	}
	return payload, nil
}

func (e *Engine) preScanGroovy(path string) ([]string, error) {
	sourceBytes, err := readSource(path)
	if err != nil {
		return nil, err
	}

	metadata := extractGroovyPipelineMetadata(string(sourceBytes))
	names := make([]string, 0)
	for _, key := range []string{"shared_libraries", "pipeline_calls", "entry_points"} {
		values, ok := metadata[key].([]string)
		if !ok {
			continue
		}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if slices.Contains(names, value) {
				continue
			}
			names = append(names, value)
		}
	}
	slices.Sort(names)
	return names, nil
}

// ExtractGroovyPipelineMetadata returns the explicit Jenkins/Groovy signals
// that the parser can safely prove from source text.
func ExtractGroovyPipelineMetadata(sourceText string) map[string]any {
	return extractGroovyPipelineMetadata(sourceText)
}

func extractGroovyPipelineMetadata(sourceText string) map[string]any {
	sharedLibraryMatches := append(
		groovyLibraryPattern.FindAllStringSubmatch(sourceText, -1),
		groovyLibraryStepPattern.FindAllStringSubmatch(sourceText, -1)...,
	)
	sharedLibraries := normalizeGroovyLibraryReferences(orderedUniqueStrings(sharedLibraryMatches, 1))
	pipelineCalls := orderedUniqueStrings(groovyPipelineCallPattern.FindAllStringSubmatch(sourceText, -1), 1)
	shellCommands := orderedUniqueStrings(groovyShellCommandPattern.FindAllStringSubmatch(sourceText, -1), 1)

	ansibleHints := make([]map[string]any, 0)
	for _, command := range shellCommands {
		matches := groovyAnsiblePattern.FindStringSubmatch(command)
		if matches == nil {
			continue
		}

		hint := map[string]any{
			"playbook": matches[1],
			"command":  command,
		}
		if strings.TrimSpace(matches[2]) != "" {
			hint["inventory"] = matches[2]
		} else {
			hint["inventory"] = nil
		}
		ansibleHints = append(ansibleHints, hint)
	}

	entryPoints := orderedUniqueStrings(groovyEntryPointPattern.FindAllStringSubmatch(sourceText, -1), 1)
	var useConfigd any
	if matches := groovyUseConfigdPattern.FindStringSubmatch(sourceText); matches != nil {
		useConfigd = matches[1] == "true"
	}

	return map[string]any{
		"shared_libraries":       sharedLibraries,
		"pipeline_calls":         pipelineCalls,
		"shell_commands":         shellCommands,
		"ansible_playbook_hints": ansibleHints,
		"entry_points":           entryPoints,
		"use_configd":            useConfigd,
		"has_pre_deploy":         groovyPreDeployPattern.MatchString(sourceText),
	}
}

func normalizeGroovyLibraryReferences(libraries []string) []string {
	normalized := make([]string, 0, len(libraries))
	for _, library := range libraries {
		library = strings.TrimSpace(library)
		if library == "" {
			continue
		}
		if at := strings.Index(library, "@"); at >= 0 {
			library = library[:at]
		}
		library = strings.TrimSpace(library)
		if library == "" {
			continue
		}
		normalized = append(normalized, library)
	}
	return normalized
}

func orderedUniqueStrings(matches [][]string, group int) []string {
	seen := make(map[string]struct{})
	ordered := make([]string, 0, len(matches))
	for _, match := range matches {
		if group >= len(match) {
			continue
		}
		value := strings.TrimSpace(match[group])
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		ordered = append(ordered, value)
	}
	return ordered
}
