// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// PRWorkflowStem returns the caller stem of a basename workflow filename.
// Both GitHub Actions YAML suffixes have the same callable identity.
func PRWorkflowStem(file string) (string, bool) {
	stem, ok := strings.CutSuffix(file, ".yml")
	if !ok {
		stem, ok = strings.CutSuffix(file, ".yaml")
	}
	return stem, ok && stem != "" && !strings.ContainsAny(stem, "/\\")
}

// ParsePRPlanFragment reads one trusted, versioned inventory fragment for
// callable workflows whose direct PR triggers were removed.
func ParsePRPlanFragment(raw []byte) ([]PRWorkflowDefinition, error) {
	var fragment struct {
		SchemaVersion int                    `yaml:"schema_version"`
		Workflows     []PRWorkflowDefinition `yaml:"workflows"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fragment); err != nil {
		return nil, fmt.Errorf("decode PR plan fragment: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("PR plan fragment has an extra document or invalid tail: %v", err)
	}
	if fragment.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported PR plan fragment schema %d", fragment.SchemaVersion)
	}
	if len(fragment.Workflows) == 0 {
		return nil, fmt.Errorf("PR plan fragment contains no workflows")
	}
	seen := make(map[string]bool)
	for _, def := range fragment.Workflows {
		if _, ok := PRWorkflowStem(def.File); !ok || filepath.Base(def.File) != def.File || seen[def.File] {
			return nil, fmt.Errorf("invalid or duplicate PR plan workflow filename %q", def.File)
		}
		seen[def.File] = true
		if def.Name == "" || !def.Synchronize || len(def.Jobs) == 0 {
			return nil, fmt.Errorf("PR plan workflow %q lacks name, synchronize, or jobs", def.File)
		}
		if len(def.Branches) > 0 && len(def.BranchesIgnore) > 0 || len(def.Paths) > 0 && len(def.PathsIgnore) > 0 {
			return nil, fmt.Errorf("PR plan workflow %q declares opposing filters", def.File)
		}
		jobIDs := make(map[string]bool)
		for _, job := range def.Jobs {
			if job == "" || jobIDs[job] {
				return nil, fmt.Errorf("PR plan workflow %q has empty or duplicate job %q", def.File, job)
			}
			jobIDs[job] = true
		}
		for job := range def.JobNames {
			if !jobIDs[job] {
				return nil, fmt.Errorf("PR plan workflow %q names unknown job %q", def.File, job)
			}
		}
	}
	return fragment.Workflows, nil
}

// ValidateCallablePRWorkflow verifies a fragment row against the callable
// workflow that will actually run. Metadata cannot preserve a removed trigger
// by merely naming a file with different jobs or an additional direct trigger.
func ValidateCallablePRWorkflow(def PRWorkflowDefinition, raw []byte) (PRWorkflowDefinition, error) {
	var wf struct {
		Name string    `yaml:"name"`
		On   yaml.Node `yaml:"on"`
		Jobs map[string]struct {
			Name  string    `yaml:"name"`
			Needs yaml.Node `yaml:"needs"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return PRWorkflowDefinition{}, fmt.Errorf("parse callable %s: %w", def.File, err)
	}
	if wf.Name != def.Name {
		return PRWorkflowDefinition{}, fmt.Errorf("callable %s name %q differs from fragment %q", def.File, wf.Name, def.Name)
	}
	if hasWorkflowEvent(&wf.On, "pull_request") || !hasWorkflowEvent(&wf.On, "workflow_call") {
		return PRWorkflowDefinition{}, fmt.Errorf("callable %s must have workflow_call and no direct pull_request trigger", def.File)
	}
	actualJobs := make([]string, 0, len(wf.Jobs))
	needs := make(map[string][]string, len(wf.Jobs))
	for job, value := range wf.Jobs {
		actualJobs = append(actualJobs, job)
		if value.Name != def.JobNames[job] {
			return PRWorkflowDefinition{}, fmt.Errorf("callable %s job %s name differs from fragment", def.File, job)
		}
		parsed, err := parsePRJobNeeds(&value.Needs)
		if err != nil {
			return PRWorkflowDefinition{}, fmt.Errorf("callable %s job %s needs: %w", def.File, job, err)
		}
		needs[job] = parsed
	}
	slices.Sort(actualJobs)
	declaredJobs := slices.Clone(def.Jobs)
	slices.Sort(declaredJobs)
	if !slices.Equal(actualJobs, declaredJobs) {
		return PRWorkflowDefinition{}, fmt.Errorf("callable %s job IDs differ from fragment", def.File)
	}
	def.Jobs = actualJobs
	def.JobNeeds = needs
	if def.JobNames == nil {
		def.JobNames = make(map[string]string)
	}
	return def, nil
}

func hasWorkflowEvent(on *yaml.Node, name string) bool {
	switch on.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(on.Content); i += 2 {
			if on.Content[i].Value == name {
				return true
			}
		}
	case yaml.SequenceNode:
		for _, event := range on.Content {
			if event.Value == name {
				return true
			}
		}
	case yaml.ScalarNode:
		return on.Value == name
	case yaml.DocumentNode, yaml.AliasNode:
		return false
	}
	return false
}

func parsePRJobNeeds(node *yaml.Node) ([]string, error) {
	switch node.Kind {
	case 0:
		return nil, nil
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			return nil, nil
		}
		if node.Value == "" {
			return nil, fmt.Errorf("empty dependency")
		}
		return []string{node.Value}, nil
	case yaml.SequenceNode:
		needs := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode || item.Value == "" {
				return nil, fmt.Errorf("non-literal dependency")
			}
			needs = append(needs, item.Value)
		}
		return needs, nil
	default:
		return nil, fmt.Errorf("unsupported dependency shape")
	}
}
