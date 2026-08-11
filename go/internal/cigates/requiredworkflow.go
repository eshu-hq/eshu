// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type requiredWorkflowStep struct {
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

type requiredWorkflowJob struct {
	Permissions map[string]string      `yaml:"permissions"`
	Steps       []requiredWorkflowStep `yaml:"steps"`
}

type requiredWorkflowFile struct {
	Permissions map[string]string              `yaml:"permissions"`
	Jobs        map[string]requiredWorkflowJob `yaml:"jobs"`
}

func checkRequiredStatusWorkflows(repoRoot string, reg *Registry) []error {
	var errs []error
	for _, check := range reg.RequiredStatusChecks {
		if check.Workflow == "" {
			continue
		}
		path := filepath.Join(repoRoot, ".github", "workflows", filepath.Base(check.Workflow))
		raw, err := os.ReadFile(path) // #nosec G304 -- basename-confined workflow from committed registry
		if err != nil {
			continue
		}
		var workflow requiredWorkflowFile
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			errs = append(errs, fmt.Errorf("required status context %q: parse workflow %q: %w", check.Context, check.Workflow, err))
			continue
		}
		job, ok := workflow.Jobs[check.Job]
		if !ok {
			errs = append(errs, fmt.Errorf(
				"required status context %q: workflow %q has no publisher job %q",
				check.Context,
				check.Workflow,
				check.Job,
			))
			continue
		}
		if !check.AggregatesBlockingGates {
			continue
		}
		errs = append(errs, validateTrustedAggregator(check, raw, workflow.Permissions, job)...)
		errs = append(errs, validateSourceWorkflow(repoRoot, check)...)
		errs = append(errs, validateBlockingWorkflowSources(repoRoot, check, raw, reg)...)
	}
	return errs
}

func validateBlockingWorkflowSources(
	repoRoot string,
	check RequiredStatusCheck,
	aggregatorRaw []byte,
	reg *Registry,
) []error {
	sources, err := workflowRunSources(aggregatorRaw)
	if err != nil {
		return []error{fmt.Errorf("required status context %q: parse workflow_run sources: %w", check.Context, err)}
	}
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	seen := make(map[string]struct{})
	var errs []error
	for _, gate := range reg.Gates {
		if !gate.Blocking || gate.CI.Workflow == "" {
			continue
		}
		if _, duplicate := seen[gate.CI.Workflow]; duplicate {
			continue
		}
		seen[gate.CI.Workflow] = struct{}{}
		path := filepath.Join(wfDir, filepath.Base(gate.CI.Workflow))
		raw, readErr := os.ReadFile(path) // #nosec G304 -- basename-confined workflow from committed registry
		if readErr != nil {
			errs = append(errs, fmt.Errorf(
				"required status context %q: read blocking workflow %q: %w",
				check.Context,
				gate.CI.Workflow,
				readErr,
			))
			continue
		}
		var identity struct {
			Name string `yaml:"name"`
		}
		if parseErr := yaml.Unmarshal(raw, &identity); parseErr != nil {
			errs = append(errs, fmt.Errorf(
				"required status context %q: parse blocking workflow %q: %w",
				check.Context,
				gate.CI.Workflow,
				parseErr,
			))
			continue
		}
		name := strings.TrimSpace(identity.Name)
		if name == "" || !slicesContain(sources, name) {
			errs = append(errs, fmt.Errorf(
				"required status context %q: workflow_run sources do not include blocking workflow %q from %s",
				check.Context,
				name,
				gate.CI.Workflow,
			))
		}
	}
	return errs
}

func validateSourceWorkflow(repoRoot string, check RequiredStatusCheck) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	var paths []string
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(wfDir, pattern))
		if err != nil {
			return []error{fmt.Errorf("required status context %q: list source workflows: %w", check.Context, err)}
		}
		paths = append(paths, matches...)
	}

	var sources [][]byte
	for _, path := range paths {
		raw, err := os.ReadFile(path) // #nosec G304 -- path is confined to the repository workflow directory
		if err != nil {
			continue
		}
		var identity struct {
			Name string `yaml:"name"`
		}
		if yaml.Unmarshal(raw, &identity) == nil && strings.TrimSpace(identity.Name) == check.SourceWorkflow {
			sources = append(sources, raw)
		}
	}
	if len(sources) != 1 {
		return []error{fmt.Errorf(
			"required status context %q: source workflow name %q resolves to %d workflow files; want exactly one",
			check.Context,
			check.SourceWorkflow,
			len(sources),
		)}
	}
	if err := requireUnfilteredPullRequest(sources[0]); err != nil {
		return []error{fmt.Errorf(
			"required status context %q: source workflow %q must have an unfiltered pull_request trigger: %w",
			check.Context,
			check.SourceWorkflow,
			err,
		)}
	}
	return nil
}

func requireUnfilteredPullRequest(raw []byte) error {
	root, err := workflowRoot(raw)
	if err != nil {
		return err
	}
	on, err := mappingValue(root, "on")
	if err != nil {
		return err
	}
	switch on.Kind {
	case yaml.ScalarNode:
		if on.Value == "pull_request" {
			return nil
		}
	case yaml.SequenceNode:
		for _, event := range on.Content {
			if event.Value == "pull_request" {
				return nil
			}
		}
	case yaml.MappingNode:
		pullRequest, err := mappingValue(on, "pull_request")
		if err != nil {
			return err
		}
		if pullRequest.Kind == yaml.ScalarNode && pullRequest.Tag == "!!null" {
			return nil
		}
		if pullRequest.Kind != yaml.MappingNode {
			return fmt.Errorf("pull_request configuration is not a mapping")
		}
		for _, filter := range []string{"paths", "paths-ignore", "branches", "branches-ignore"} {
			if _, err := mappingValue(pullRequest, filter); err == nil {
				return fmt.Errorf("pull_request declares %s", filter)
			}
		}
		return nil
	case yaml.DocumentNode, yaml.AliasNode:
		return fmt.Errorf("on trigger has unsupported YAML node kind %d", on.Kind)
	}
	return fmt.Errorf("pull_request trigger is missing")
}

func validateTrustedAggregator(
	check RequiredStatusCheck,
	raw []byte,
	workflowPermissions map[string]string,
	job requiredWorkflowJob,
) []error {
	var errs []error
	triggers, err := workflowTriggerKeys(raw)
	if err != nil {
		return []error{fmt.Errorf("required status context %q: parse workflow triggers: %w", check.Context, err)}
	}
	if _, ok := triggers["workflow_run"]; !ok {
		errs = append(errs, fmt.Errorf("required status context %q: trusted publisher must trigger on workflow_run", check.Context))
	}
	sources, sourceErr := workflowRunSources(raw)
	if sourceErr != nil {
		errs = append(errs, fmt.Errorf("required status context %q: parse workflow_run sources: %w", check.Context, sourceErr))
	} else if !slicesContain(sources, check.SourceWorkflow) {
		errs = append(errs, fmt.Errorf(
			"required status context %q: workflow_run sources %q do not include declared source workflow %q",
			check.Context,
			sources,
			check.SourceWorkflow,
		))
	}
	types, typesErr := workflowRunList(raw, "types")
	if typesErr != nil {
		errs = append(errs, fmt.Errorf("required status context %q: parse workflow_run activity types: %w", check.Context, typesErr))
	} else {
		for _, requiredType := range []string{"in_progress", "completed"} {
			if !slicesContain(types, requiredType) {
				errs = append(errs, fmt.Errorf(
					"required status context %q: workflow_run activity types must include %q",
					check.Context,
					requiredType,
				))
			}
		}
	}
	for _, forbidden := range []string{"pull_request", "pull_request_target"} {
		if _, ok := triggers[forbidden]; ok {
			errs = append(errs, fmt.Errorf(
				"required status context %q: trusted publisher must not execute from untrusted %s workflow code",
				check.Context,
				forbidden,
			))
		}
	}

	permissions := workflowPermissions
	if len(job.Permissions) > 0 {
		permissions = job.Permissions
	}
	for permission, want := range map[string]string{
		"actions":       "read",
		"checks":        "read",
		"contents":      "read",
		"pull-requests": "read",
		"statuses":      "write",
	} {
		if got := permissions[permission]; got != want {
			errs = append(errs, fmt.Errorf(
				"required status context %q: publisher permission %s=%q; want %q",
				check.Context,
				permission,
				got,
				want,
			))
		}
	}

	var commands strings.Builder
	pendingIndex := -1
	awaitIndex := -1
	terminalIndex := -1
	for _, step := range job.Steps {
		index := commands.Len()
		commands.WriteString(step.Run)
		commands.WriteByte('\n')
		if strings.Contains(step.Run, "${{") {
			errs = append(errs, fmt.Errorf(
				"required status context %q: shell scripts must receive GitHub expression values through env",
				check.Context,
			))
		}
		if strings.Contains(step.Run, "/statuses/") && strings.Contains(step.Run, "state=pending") {
			pendingIndex = index
			if !strings.Contains(step.Env["HEAD_SHA"], "workflow_run.head_sha") {
				errs = append(errs, fmt.Errorf(
					"required status context %q: pending status must target github.event.workflow_run.head_sha",
					check.Context,
				))
			}
		}
		if strings.Contains(step.Run, "ci-gates await") {
			awaitIndex = index
		}
		if strings.Contains(step.Run, "/statuses/") && strings.Contains(step.Run, "state=failure") {
			terminalIndex = index
			condition := strings.TrimSpace(step.If)
			hasExpressionDelimiters := strings.HasPrefix(condition, "${{") && strings.HasSuffix(condition, "}}")
			if hasExpressionDelimiters {
				condition = strings.TrimSpace(condition[3 : len(condition)-2])
			}
			if !hasExpressionDelimiters || condition != "!cancelled()" {
				errs = append(errs, fmt.Errorf(
					"required status context %q: terminal status publisher must use !cancelled() to run after failure without publishing on cancellation",
					check.Context,
				))
			}
			if !strings.Contains(step.Env["HEAD_SHA"], "workflow_run.head_sha") {
				errs = append(errs, fmt.Errorf(
					"required status context %q: terminal status must target github.event.workflow_run.head_sha",
					check.Context,
				))
			}
		}
		if strings.HasPrefix(step.Uses, "actions/checkout@") {
			if ref := strings.TrimSpace(step.With["ref"]); ref != "" && !strings.Contains(ref, "default_branch") {
				errs = append(errs, fmt.Errorf(
					"required status context %q: trusted publisher checkout ref %q is not the default branch",
					check.Context,
					ref,
				))
			}
		}
	}
	if pendingIndex < 0 || awaitIndex < 0 || terminalIndex < 0 || pendingIndex >= awaitIndex || awaitIndex >= terminalIndex {
		errs = append(errs, fmt.Errorf(
			"required status context %q: publisher must post pending before await and a cancellation-safe terminal status afterward",
			check.Context,
		))
	}
	commandText := commands.String()
	for _, required := range []string{"ci-gates await", "/statuses/", "context=" + check.Context} {
		if !strings.Contains(commandText, required) {
			errs = append(errs, fmt.Errorf(
				"required status context %q: publisher job %q does not execute %q",
				check.Context,
				check.Job,
				required,
			))
		}
	}
	if strings.Contains(string(raw), "${{ secrets.") {
		errs = append(errs, fmt.Errorf("required status context %q: trusted publisher must not consume repository secrets", check.Context))
	}
	return errs
}

func workflowTriggerKeys(raw []byte) (map[string]struct{}, error) {
	root, err := workflowRoot(raw)
	if err != nil {
		return nil, err
	}
	on, err := mappingValue(root, "on")
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{})
	switch on.Kind {
	case yaml.ScalarNode:
		keys[on.Value] = struct{}{}
	case yaml.SequenceNode:
		for _, event := range on.Content {
			keys[event.Value] = struct{}{}
		}
	case yaml.MappingNode:
		for j := 0; j+1 < len(on.Content); j += 2 {
			keys[on.Content[j].Value] = struct{}{}
		}
	default:
		return nil, fmt.Errorf("on trigger has unsupported YAML node kind %d", on.Kind)
	}
	return keys, nil
}

func workflowRunSources(raw []byte) ([]string, error) {
	return workflowRunList(raw, "workflows")
}

func workflowRunList(raw []byte, key string) ([]string, error) {
	root, err := workflowRoot(raw)
	if err != nil {
		return nil, err
	}
	on, err := mappingValue(root, "on")
	if err != nil {
		return nil, err
	}
	workflowRun, err := mappingValue(on, "workflow_run")
	if err != nil {
		return nil, err
	}
	valuesNode, err := mappingValue(workflowRun, key)
	if err != nil {
		return nil, err
	}
	var values []string
	switch valuesNode.Kind {
	case yaml.ScalarNode:
		values = append(values, strings.TrimSpace(valuesNode.Value))
	case yaml.SequenceNode:
		for _, value := range valuesNode.Content {
			values = append(values, strings.TrimSpace(value.Value))
		}
	default:
		return nil, fmt.Errorf("workflow_run.%s must be a string or sequence", key)
	}
	return values, nil
}

func workflowRoot(raw []byte) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}
	if len(document.Content) == 0 || len(document.Content[0].Content) == 0 {
		return nil, fmt.Errorf("empty workflow document")
	}
	return document.Content[0], nil
}

func mappingValue(node *yaml.Node, key string) (*yaml.Node, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s parent is not a mapping", key)
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], nil
		}
	}
	return nil, fmt.Errorf("workflow has no %s key", key)
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
