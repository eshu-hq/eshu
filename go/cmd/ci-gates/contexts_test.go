// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestContextsSubcommand_JSON(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	dir := t.TempDir()
	registry := writeRegistry(t, dir, `version: v1
gates: []
required_status_checks:
  - context: required-gates-complete
    workflow: required-gates.yml
    job: aggregate
    source_workflow: Build Test
    integration_id: 15368
    aggregates_blocking_gates: true
`)
	cmd := exec.Command(bin, "contexts", "--registry", registry, "--json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("contexts failed: %v\n%s", err, output)
	}
	var got []struct {
		Context       string `json:"context"`
		IntegrationID int64  `json:"integration_id"`
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode contexts JSON: %v\n%s", err, output)
	}
	if len(got) != 1 || got[0].Context != "required-gates-complete" || got[0].IntegrationID != 15368 {
		t.Fatalf("contexts = %#v", got)
	}
}

func TestContextsSubcommand_RejectsInvalidRequiredStatusManifest(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	dir := t.TempDir()
	registry := writeRegistry(t, dir, `version: v1
gates: []
required_status_checks:
  - context: required-gates-complete
    workflow: required-gates.yml
    job: aggregate
    integration_id: 15368
    aggregates_blocking_gates: true
`)
	cmd := exec.Command(bin, "contexts", "--registry", registry, "--json")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("invalid required-status manifest should fail, output: %s", output)
	}
}

const rulesetOnlyRegistry = `version: v1
gates: []
required_status_checks:
  - context: go-core-complete
    workflow: test.yml
    job: go-core-complete
    integration_id: 15368
  - context: required-gates-complete
    workflow: required-gates.yml
    job: aggregate
    source_workflow: Build Test
    integration_id: 15368
    aggregates_blocking_gates: true
    ruleset_required: false
`

type contextsJSON struct {
	Context         string `json:"context"`
	IntegrationID   int64  `json:"integration_id"`
	RulesetRequired bool   `json:"ruleset_required"`
}

func runContextsJSON(t *testing.T, extra ...string) []contextsJSON {
	t.Helper()
	bin := buildBinary(t)
	registry := writeRegistry(t, t.TempDir(), rulesetOnlyRegistry)
	args := append([]string{"contexts", "--registry", registry, "--json"}, extra...)
	output, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("contexts failed: %v\n%s", err, output)
	}
	var got []contextsJSON
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode contexts JSON: %v\n%s", err, output)
	}
	return got
}

func TestContextsSubcommand_JSONEmitsRulesetRequired(t *testing.T) {
	t.Parallel()

	got := runContextsJSON(t)
	if len(got) != 2 {
		t.Fatalf("contexts = %#v; want both entries", got)
	}
	if !got[0].RulesetRequired || got[1].RulesetRequired {
		t.Fatalf("ruleset_required = %#v; want [true false]", got)
	}
}

func TestContextsSubcommand_RulesetOnlyExcludesNonRulesetEntries(t *testing.T) {
	t.Parallel()

	got := runContextsJSON(t, "--ruleset-only")
	if len(got) != 1 || got[0].Context != "go-core-complete" {
		t.Fatalf("contexts --ruleset-only = %#v; want only go-core-complete", got)
	}
}
