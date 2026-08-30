// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

// YAML-only registry shapes stay separate from the public gate model so
// registry.go remains focused on the runtime contract and load validation.
type registryFile struct {
	Version              string                    `yaml:"version"`
	Gates                []gateFile                `yaml:"gates"`
	HygieneHooks         []hygieneHookFile         `yaml:"hygiene_hooks"`
	NonGateWorkflows     []nonGateWorkflowFile     `yaml:"non_gate_workflows"`
	RequiredStatusChecks []requiredStatusCheckFile `yaml:"required_status_checks"`
}

type gateFile struct {
	ID               string     `yaml:"id"`
	Name             string     `yaml:"name"`
	Category         string     `yaml:"category"`
	Tier             string     `yaml:"tier"`
	Blocking         bool       `yaml:"blocking"`
	Triggers         []string   `yaml:"triggers"`
	SelfTestTriggers *[]string  `yaml:"self_test_triggers"`
	Local            *localFile `yaml:"local"`
	CI               ciFile     `yaml:"ci"`
	Requirements     []string   `yaml:"requirements"`
	CIOnlyReason     string     `yaml:"ci_only_reason"`
	LocalOnlyReason  string     `yaml:"local_only_reason"`
	HookID           string     `yaml:"hook_id"`
}

type localFile struct {
	Command     string `yaml:"command"`
	TestCommand string `yaml:"test_command"`
}

type ciFile struct {
	Workflow   string   `yaml:"workflow"`
	Job        string   `yaml:"job"`
	CheckNames []string `yaml:"check_names"`
}

type hygieneHookFile struct {
	ID     string `yaml:"id"`
	Reason string `yaml:"reason"`
}

type nonGateWorkflowFile struct {
	File   string `yaml:"file"`
	Reason string `yaml:"reason"`
}

type requiredStatusCheckFile struct {
	Context                 string `yaml:"context"`
	Workflow                string `yaml:"workflow"`
	Job                     string `yaml:"job"`
	SourceWorkflow          string `yaml:"source_workflow"`
	IntegrationID           int64  `yaml:"integration_id"`
	AggregatesBlockingGates bool   `yaml:"aggregates_blocking_gates"`
}
