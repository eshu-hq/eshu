// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PRWorkflowDefinition is the trusted, parsed pull-request scheduling contract
// for one workflow. Synchronize is false for opened-only and labeled-only jobs.
type PRWorkflowDefinition struct {
	File           string              `yaml:"file"`
	Name           string              `yaml:"name"`
	Synchronize    bool                `yaml:"synchronize"`
	Branches       []string            `yaml:"branches"`
	BranchesIgnore []string            `yaml:"branches_ignore"`
	Paths          []string            `yaml:"paths"`
	PathsIgnore    []string            `yaml:"paths_ignore"`
	Jobs           []string            `yaml:"jobs"`
	JobNames       map[string]string   `yaml:"job_names"`
	JobNeeds       map[string][]string `yaml:"-"`
}

// PlanPRWorkflows selects local job IDs from trusted workflow definitions.
// The caller must independently prove reuse or smoke before requesting those
// modes. A full plan retains normal synchronize path filters and non-gate jobs.
func PlanPRWorkflows(defs []PRWorkflowDefinition, paths []string, truncated bool, baseRef, mode string, fastJobs map[string][]string) (map[string][]string, error) {
	if mode != "full" && mode != "reuse" && mode != "smoke" {
		return nil, fmt.Errorf("unsupported PR plan mode %q", mode)
	}
	if baseRef == "" {
		return nil, fmt.Errorf("PR plan has no base branch")
	}
	if len(paths) == 0 && mode != "reuse" {
		return nil, fmt.Errorf("PR plan has no changed paths")
	}
	planned := make(map[string][]string)
	seen := make(map[string]bool)
	for _, def := range defs {
		if def.File == "" || def.Name == "" || seen[def.File] || len(def.Jobs) == 0 {
			return nil, fmt.Errorf("invalid or duplicate PR workflow %q", def.File)
		}
		seen[def.File] = true
		if !def.Synchronize || mode == "reuse" || !workflowBranchMatch(def, baseRef) || !workflowPathsMatch(def, paths, truncated) {
			continue
		}
		if mode == "full" {
			planned[def.File] = slices.Clone(def.Jobs)
			continue
		}
		for _, job := range fastJobs[def.File] {
			if !slices.Contains(def.Jobs, job) {
				return nil, fmt.Errorf("smoke job %q is absent from %s", job, def.File)
			}
			if !slices.Contains(planned[def.File], job) {
				planned[def.File] = append(planned[def.File], job)
			}
		}
		if len(planned[def.File]) > 0 {
			closed, err := closeSmokeJobNeeds(def, planned[def.File])
			if err != nil {
				return nil, err
			}
			planned[def.File] = closed
		}
	}
	if mode == "smoke" {
		for workflow, jobs := range fastJobs {
			if len(jobs) > 0 && len(planned[workflow]) == 0 {
				return nil, fmt.Errorf("smoke workflow %q does not run on this PR event", workflow)
			}
		}
	}
	if mode == "smoke" && len(planned) == 0 {
		return nil, fmt.Errorf("smoke plan has no proven fast jobs")
	}
	if mode == "full" && len(planned) == 0 {
		return nil, fmt.Errorf("full PR plan selected no synchronize workflow")
	}
	return planned, nil
}

func workflowPathsMatch(def PRWorkflowDefinition, paths []string, truncated bool) bool {
	if truncated {
		return true
	}
	if len(def.Paths) == 0 && len(def.PathsIgnore) == 0 {
		return true
	}
	for _, path := range paths {
		if len(def.Paths) > 0 {
			selected := false
			for _, pattern := range def.Paths {
				if len(pattern) > 0 && pattern[0] == '!' {
					if MatchGlob(pattern[1:], path) {
						selected = false
					}
				} else if MatchGlob(pattern, path) {
					selected = true
				}
			}
			if selected {
				return true
			}
			continue
		}
		ignored := false
		for _, pattern := range def.PathsIgnore {
			if MatchGlob(pattern, path) {
				ignored = true
			}
		}
		if !ignored {
			return true
		}
	}
	return false
}

func workflowBranchMatch(def PRWorkflowDefinition, baseRef string) bool {
	if len(def.Branches) > 0 {
		selected := false
		for _, pattern := range def.Branches {
			if len(pattern) > 0 && pattern[0] == '!' {
				if MatchGlob(pattern[1:], baseRef) {
					selected = false
				}
			} else if MatchGlob(pattern, baseRef) {
				selected = true
			}
		}
		return selected
	}
	for _, pattern := range def.BranchesIgnore {
		if MatchGlob(pattern, baseRef) {
			return false
		}
	}
	return true
}

// HasUnknownPRWorkflowDefinitions reports whether a trusted PR workflow lacks
// a registry gate, required status, or explicit non-gate inventory owner.
// Unknown definitions forbid verdict transfer; a full plan still includes them.
func HasUnknownPRWorkflowDefinitions(reg *Registry, defs []PRWorkflowDefinition) bool {
	known := make(map[string]bool)
	for _, gate := range reg.Gates {
		if gate.CI.Workflow != "" {
			known[gate.CI.Workflow] = true
		}
	}
	for _, check := range reg.RequiredStatusChecks {
		if check.Workflow != "" {
			known[check.Workflow] = true
		}
	}
	for _, nonGate := range reg.NonGateWorkflows {
		known[nonGate.File] = true
	}
	for _, def := range defs {
		if !known[def.File] {
			return true
		}
	}
	return false
}

// ParsePRWorkflowDefinition reads one trusted workflow's pull_request event,
// ordered path filters, and local job IDs. Workflows without pull_request are
// excluded; event types other than synchronize are retained but not planned.
func ParsePRWorkflowDefinition(file string, raw []byte) (PRWorkflowDefinition, bool, error) {
	if filepath.Base(file) != file || (!strings.HasSuffix(file, ".yml") && !strings.HasSuffix(file, ".yaml")) {
		return PRWorkflowDefinition{}, false, fmt.Errorf("invalid workflow filename %q", file)
	}
	var wf struct {
		Name string    `yaml:"name"`
		On   yaml.Node `yaml:"on"`
		Jobs map[string]struct {
			Name  string    `yaml:"name"`
			Needs yaml.Node `yaml:"needs"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return PRWorkflowDefinition{}, false, fmt.Errorf("parse %s: %w", file, err)
	}
	if strings.TrimSpace(wf.Name) == "" {
		return PRWorkflowDefinition{}, false, fmt.Errorf("workflow %s has no name", file)
	}
	pull, present, err := pullRequestNode(&wf.On)
	if err != nil || !present {
		return PRWorkflowDefinition{}, false, err
	}
	def := PRWorkflowDefinition{File: file, Name: wf.Name, Synchronize: true, JobNames: make(map[string]string), JobNeeds: make(map[string][]string)}
	if pull.Kind == yaml.MappingNode {
		var event struct {
			Types          []string `yaml:"types"`
			Branches       []string `yaml:"branches"`
			BranchesIgnore []string `yaml:"branches-ignore"`
			Paths          []string `yaml:"paths"`
			PathsIgnore    []string `yaml:"paths-ignore"`
		}
		if err := pull.Decode(&event); err != nil {
			return PRWorkflowDefinition{}, false, fmt.Errorf("parse pull_request in %s: %w", file, err)
		}
		if len(event.Branches) > 0 && len(event.BranchesIgnore) > 0 {
			return PRWorkflowDefinition{}, false, fmt.Errorf("workflow %s declares branches and branches-ignore", file)
		}
		if len(event.Paths) > 0 && len(event.PathsIgnore) > 0 {
			return PRWorkflowDefinition{}, false, fmt.Errorf("workflow %s declares paths and paths-ignore", file)
		}
		def.Paths, def.PathsIgnore = event.Paths, event.PathsIgnore
		def.Branches, def.BranchesIgnore = event.Branches, event.BranchesIgnore
		if len(event.Types) > 0 {
			def.Synchronize = false
			for _, typ := range event.Types {
				if typ == "synchronize" {
					def.Synchronize = true
				}
			}
		}
	} else if pull.Kind != yaml.ScalarNode || pull.Tag != "!!null" {
		return PRWorkflowDefinition{}, false, fmt.Errorf("unsupported pull_request shape in %s", file)
	}
	for job, value := range wf.Jobs {
		def.Jobs = append(def.Jobs, job)
		def.JobNames[job] = value.Name
		needs, err := parsePRJobNeeds(&value.Needs)
		if err != nil {
			return PRWorkflowDefinition{}, false, fmt.Errorf("workflow %s job %s needs: %w", file, job, err)
		}
		def.JobNeeds[job] = needs
	}
	sort.Strings(def.Jobs)
	if len(def.Jobs) == 0 {
		return PRWorkflowDefinition{}, false, fmt.Errorf("workflow %s has no jobs", file)
	}
	return def, true, nil
}

func pullRequestNode(on *yaml.Node) (*yaml.Node, bool, error) {
	switch on.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(on.Content); i += 2 {
			if on.Content[i].Value == "pull_request" {
				return on.Content[i+1], true, nil
			}
		}
	case yaml.SequenceNode:
		for _, event := range on.Content {
			if event.Value == "pull_request" {
				return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}, true, nil
			}
		}
	case yaml.ScalarNode:
		if on.Value == "pull_request" {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}, true, nil
		}
	default:
		return nil, false, fmt.Errorf("unsupported workflow trigger shape")
	}
	return nil, false, nil
}

// SelectPRGateIDs names the registry rows whose CI cells the plan may run.
// Jobs alone cannot distinguish matrix cells sharing one local job ID. Full
// mode keeps every path-selected row of an applicable workflow; smoke keeps
// only fast rows mapped to one selected local job; reuse keeps none.
func SelectPRGateIDs(reg *Registry, defs []PRWorkflowDefinition, paths []string, truncated bool, mode string, jobs map[string][]string) ([]string, error) {
	ids := make([]string, 0)
	if mode == "reuse" {
		return ids, nil
	}
	if mode != "full" && mode != "smoke" {
		return nil, fmt.Errorf("unsupported PR gate plan mode %q", mode)
	}
	defByFile := make(map[string]PRWorkflowDefinition, len(defs))
	for _, def := range defs {
		defByFile[def.File] = def
	}
	for _, gate := range reg.Gates {
		workflow := gate.CI.Workflow
		if workflow == "" || len(jobs[workflow]) == 0 || !truncated && !gateMatchesAnyPath(gate, paths) {
			continue
		}
		if gate.CI.Job == "" {
			return nil, fmt.Errorf("gate %q has a workflow but no local job owner", gate.ID)
		}
		if mode == "smoke" {
			if gate.Tier != TierPreCommit && gate.Tier != TierPrePush {
				continue
			}
			def, ok := defByFile[workflow]
			if !ok {
				return nil, fmt.Errorf("smoke workflow %s is absent from trusted definitions", workflow)
			}
			localID := ""
			for _, job := range def.Jobs {
				if job == gate.CI.Job || def.JobNames[job] == gate.CI.Job {
					if localID != "" {
						return nil, fmt.Errorf("smoke gate %s has ambiguous local job", gate.ID)
					}
					localID = job
				}
			}
			if localID == "" {
				return nil, fmt.Errorf("smoke gate %s has no known local job", gate.ID)
			}
			if !slices.Contains(jobs[workflow], localID) {
				continue
			}
		}
		ids = append(ids, gate.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

func closeSmokeJobNeeds(def PRWorkflowDefinition, selected []string) ([]string, error) {
	included := make(map[string]bool)
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string) error
	visit = func(job string) error {
		if visiting[job] {
			return fmt.Errorf("smoke workflow %s has a job dependency cycle at %s", def.File, job)
		}
		if visited[job] {
			return nil
		}
		if !slices.Contains(def.Jobs, job) {
			return fmt.Errorf("smoke workflow %s needs unknown job %s", def.File, job)
		}
		visiting[job] = true
		included[job] = true
		// These required-status aggregates intentionally accept transferred
		// heavy results. Their `if: always()` jobs may need a skipped heavy
		// job, but selecting that job would run the heavy lane again.
		if def.File != "test.yml" || (job != "go-core-complete" && job != "go-race-complete") {
			for _, dependency := range def.JobNeeds[job] {
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		visiting[job] = false
		visited[job] = true
		return nil
	}
	for _, job := range selected {
		if err := visit(job); err != nil {
			return nil, err
		}
	}
	closed := make([]string, 0, len(included))
	for job := range included {
		closed = append(closed, job)
	}
	sort.Strings(closed)
	return closed, nil
}
