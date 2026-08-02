// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestValidateRequiredStatusChecks_RequiresBlockingAggregator(t *testing.T) {
	t.Parallel()

	reg := &cigates.Registry{
		Gates: []cigates.Gate{{
			ID:       "contract",
			Blocking: true,
			CI:       cigates.CI{Workflow: "test.yml", Job: "contract"},
		}},
		RequiredStatusChecks: []cigates.RequiredStatusCheck{{
			Context:       "go-core-complete",
			Workflow:      "test.yml",
			Job:           "go-core-complete",
			IntegrationID: 15368,
		}},
	}

	errs := reg.ValidateRequiredStatusChecks()
	if len(errs) == 0 {
		t.Fatal("expected missing blocking-gate aggregator to fail validation")
	}
	if !strings.Contains(errs[0].Error(), "aggregates_blocking_gates") {
		t.Fatalf("error %q should explain the missing aggregator", errs[0])
	}
}

func TestValidateRequiredStatusChecks_RejectsBlockingGateWithoutCIReachability(t *testing.T) {
	t.Parallel()

	reg := &cigates.Registry{
		Gates: []cigates.Gate{{ID: "local-only", Blocking: true}},
		RequiredStatusChecks: []cigates.RequiredStatusCheck{{
			Context:                 "required-gates-complete",
			Workflow:                "required-gates.yml",
			Job:                     "aggregate",
			SourceWorkflow:          "Build Test",
			IntegrationID:           15368,
			AggregatesBlockingGates: true,
		}},
	}

	errs := reg.ValidateRequiredStatusChecks()
	if len(errs) == 0 {
		t.Fatal("expected blocking gate without ci.workflow/ci.job to fail validation")
	}
	if !strings.Contains(errs[0].Error(), "local-only") {
		t.Fatalf("error %q should name the unreachable blocking gate", errs[0])
	}
}

func TestValidateRequiredStatusChecks_RejectsWorkflowPathTraversal(t *testing.T) {
	t.Parallel()

	reg := &cigates.Registry{RequiredStatusChecks: []cigates.RequiredStatusCheck{{
		Context:                 "required-gates-complete",
		Workflow:                "../required-gates.yml",
		Job:                     "aggregate",
		SourceWorkflow:          "Build Test",
		IntegrationID:           15368,
		AggregatesBlockingGates: true,
	}}}

	errs := reg.ValidateRequiredStatusChecks()
	if len(errs) == 0 {
		t.Fatal("required status workflow must be confined to .github/workflows")
	}
	if !strings.Contains(errs[0].Error(), "filename") {
		t.Fatalf("workflow path error should require a filename: %v", errs)
	}
}

func TestLoad_RequiredStatusChecks(t *testing.T) {
	t.Parallel()

	path := writeYAML(t, minimalValidYAML+`required_status_checks:
  - context: required-gates-complete
    workflow: required-gates.yml
    job: aggregate
    source_workflow: Build Test
    integration_id: 15368
    aggregates_blocking_gates: true
`)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(reg.RequiredStatusChecks) != 1 {
		t.Fatalf("required status checks = %d; want 1", len(reg.RequiredStatusChecks))
	}
	got := reg.RequiredStatusChecks[0]
	if got.Context != "required-gates-complete" || got.SourceWorkflow != "Build Test" || !got.AggregatesBlockingGates {
		t.Fatalf("required status check = %#v", got)
	}
}

func TestValidate_RequiredStatusWorkflowExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := &cigates.Registry{RequiredStatusChecks: []cigates.RequiredStatusCheck{{
		Context:                 "required-gates-complete",
		Workflow:                "required-gates.yml",
		Job:                     "aggregate",
		SourceWorkflow:          "Build Test",
		IntegrationID:           15368,
		AggregatesBlockingGates: true,
	}}}

	errs := reg.Validate(root)
	if len(errs) == 0 {
		t.Fatal("expected missing required-status workflow to fail validation")
	}
	if !strings.Contains(errs[0].Error(), "required-gates.yml") {
		t.Fatalf("error %q should name the missing required-status workflow", errs[0])
	}
}
