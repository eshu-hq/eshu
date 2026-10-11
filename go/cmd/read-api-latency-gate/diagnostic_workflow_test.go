// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDiagnosticWorkflowIsManualAndDefaultsOff(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/read-api-latency-gate.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On struct {
			Dispatch struct {
				Inputs map[string]struct {
					Type    string `yaml:"type"`
					Default bool   `yaml:"default"`
				} `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
		} `yaml:"on"`
		Jobs map[string]struct {
			If    string `yaml:"if"`
			Steps []struct {
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	input, exists := workflow.On.Dispatch.Inputs["status_diagnostic"]
	if !exists || input.Type != "boolean" || input.Default {
		t.Fatal("manual diagnostic input must exist as a boolean defaulting off")
	}
	bindings := 0
	for name, job := range workflow.Jobs {
		for _, step := range job.Steps {
			expression, exists := step.Env["GATE_STATUS_DIAGNOSTIC_DIR"]
			if !exists {
				continue
			}
			if name != "methodology-scale" || job.If != "github.event_name == 'workflow_dispatch'" || expression != "${{ inputs.status_diagnostic && format('{0}/methodology-status-diagnostics', runner.temp) || '' }}" {
				t.Fatal("diagnostic mode escaped the dedicated manual job or disabled default")
			}
			bindings++
		}
	}
	if bindings != 1 {
		t.Fatalf("expected one diagnostic binding, got %d", bindings)
	}
}
