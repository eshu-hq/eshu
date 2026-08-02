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
