package groovy

import (
	"regexp"
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

// Metadata is the Jenkins/Groovy delivery evidence extracted from source text.
type Metadata struct {
	SharedLibraries      []string
	PipelineCalls        []string
	ShellCommands        []string
	AnsiblePlaybookHints []AnsiblePlaybookHint
	EntryPoints          []string
	UseConfigd           *bool
	HasPreDeploy         bool
}

// AnsiblePlaybookHint records an ansible-playbook invocation found in a Groovy
// shell command.
type AnsiblePlaybookHint struct {
	Playbook  string
	Command   string
	Inventory any
}

// PipelineMetadata returns the explicit Jenkins/Groovy signals that the parser
// can safely prove from source text.
func PipelineMetadata(sourceText string) Metadata {
	sharedLibraryMatches := append(
		groovyLibraryPattern.FindAllStringSubmatch(sourceText, -1),
		groovyLibraryStepPattern.FindAllStringSubmatch(sourceText, -1)...,
	)
	sharedLibraries := normalizeGroovyLibraryReferences(orderedUniqueStrings(sharedLibraryMatches, 1))
	pipelineCalls := orderedUniqueStrings(groovyPipelineCallPattern.FindAllStringSubmatch(sourceText, -1), 1)
	shellCommands := orderedUniqueStrings(groovyShellCommandPattern.FindAllStringSubmatch(sourceText, -1), 1)

	ansibleHints := make([]AnsiblePlaybookHint, 0)
	for _, command := range shellCommands {
		matches := groovyAnsiblePattern.FindStringSubmatch(command)
		if matches == nil {
			continue
		}

		var inventory any
		if strings.TrimSpace(matches[2]) != "" {
			inventory = matches[2]
		}
		ansibleHints = append(ansibleHints, AnsiblePlaybookHint{
			Playbook:  matches[1],
			Command:   command,
			Inventory: inventory,
		})
	}

	entryPoints := orderedUniqueStrings(groovyEntryPointPattern.FindAllStringSubmatch(sourceText, -1), 1)
	var useConfigd *bool
	if matches := groovyUseConfigdPattern.FindStringSubmatch(sourceText); matches != nil {
		value := matches[1] == "true"
		useConfigd = &value
	}

	return Metadata{
		SharedLibraries:      sharedLibraries,
		PipelineCalls:        pipelineCalls,
		ShellCommands:        shellCommands,
		AnsiblePlaybookHints: ansibleHints,
		EntryPoints:          entryPoints,
		UseConfigd:           useConfigd,
		HasPreDeploy:         groovyPreDeployPattern.MatchString(sourceText),
	}
}

// Map returns the parent parser payload shape used by existing query and
// relationship callers.
func (m Metadata) Map() map[string]any {
	ansibleHints := make([]map[string]any, 0, len(m.AnsiblePlaybookHints))
	for _, hint := range m.AnsiblePlaybookHints {
		ansibleHints = append(ansibleHints, map[string]any{
			"playbook":  hint.Playbook,
			"command":   hint.Command,
			"inventory": hint.Inventory,
		})
	}
	var useConfigd any
	if m.UseConfigd != nil {
		useConfigd = *m.UseConfigd
	}
	return map[string]any{
		"shared_libraries":       m.SharedLibraries,
		"pipeline_calls":         m.PipelineCalls,
		"shell_commands":         m.ShellCommands,
		"ansible_playbook_hints": ansibleHints,
		"entry_points":           m.EntryPoints,
		"use_configd":            useConfigd,
		"has_pre_deploy":         m.HasPreDeploy,
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
