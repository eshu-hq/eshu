// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const trustedRequiredWorkflow = `name: Required Gates
on:
  workflow_run:
    workflows: ["Build Test"]
    types: [in_progress, completed]
concurrency:
  group: required-gates-${{ github.event.workflow_run.head_sha || github.ref }}
  cancel-in-progress: false
permissions:
  actions: read
  checks: read
  contents: read
  pull-requests: read
  statuses: write
jobs:
  aggregate:
    runs-on: ubuntu-latest
    steps:
      - name: Publish pending
        env:
          HEAD_SHA: ${{ github.event.workflow_run.head_sha }}
        run: gh api -X POST repos/example/repo/statuses/${HEAD_SHA} -f state=pending -f context=required-gates-complete
      - name: Await blockers
        run: go run ./cmd/ci-gates await
      - name: Publish terminal
        if: ${{ !cancelled() }}
        env:
          HEAD_SHA: ${{ github.event.workflow_run.head_sha }}
        run: gh api -X POST repos/example/repo/statuses/${HEAD_SHA} -f state=failure -f context=required-gates-complete
`

func writeRequiredWorkflowFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "required-gates.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "test.yml"),
		[]byte("name: Build Test\non:\n  pull_request:\njobs: {}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	return root
}

func requiredWorkflowRegistry() *Registry {
	return &Registry{RequiredStatusChecks: []RequiredStatusCheck{{
		Context:                 "required-gates-complete",
		Workflow:                "required-gates.yml",
		Job:                     "aggregate",
		SourceWorkflow:          "Build Test",
		IntegrationID:           15368,
		AggregatesBlockingGates: true,
	}}}
}

func TestCheckRequiredStatusWorkflows_TrustedPublisherPasses(t *testing.T) {
	t.Parallel()

	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, trustedRequiredWorkflow), requiredWorkflowRegistry())
	if len(errs) != 0 {
		t.Fatalf("trusted required-status publisher returned errors: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_AcceptsEquivalentCancellationSpacing(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, `${{ !cancelled() }}`, `${{!cancelled()}}`, 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) != 0 {
		t.Fatalf("equivalent cancellation-safe condition returned errors: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_RejectsPullRequestTarget(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, "  workflow_run:\n", "  pull_request_target:\n  workflow_run:\n", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("pull_request_target must be rejected for the trusted status publisher")
	}
}

func TestCheckRequiredStatusWorkflows_RequiresStatusWriteAndAwait(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, "  statuses: write\n", "", 1)
	body = strings.Replace(body, "go run ./cmd/ci-gates await", "echo bypass", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) < 2 {
		t.Fatalf("expected status permission and await-command errors, got: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_RequiresChecksRead(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, "  checks: read\n", "", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("trusted publisher needs checks: read for gh pr checks")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "checks") && strings.Contains(err.Error(), "read") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected checks permission error, got: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_RequiresDeclaredSourceWorkflow(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, `workflows: ["Build Test"]`, `workflows: ["Untrusted Trigger"]`, 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("trusted publisher must listen to its declared source workflow")
	}
	if !strings.Contains(errs[0].Error(), "Build Test") {
		t.Fatalf("source-workflow error should name Build Test: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_RejectsFilteredSourceWorkflow(t *testing.T) {
	t.Parallel()

	root := writeRequiredWorkflowFixture(t, trustedRequiredWorkflow)
	body := "name: Build Test\non:\n  pull_request:\n    paths:\n      - 'go/**'\njobs: {}\n"
	path := filepath.Join(root, ".github", "workflows", "test.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	errs := checkRequiredStatusWorkflows(root, requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("path-filtered source workflow must be rejected")
	}
	if !strings.Contains(errs[0].Error(), "unfiltered pull_request") {
		t.Fatalf("source reachability error should explain unfiltered pull_request: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_RequiresTerminalFailureAfterSetupFailure(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, `if: ${{ !cancelled() }}`, `if: ${{ success() }}`, 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("terminal failure publisher must run even when setup or aggregation fails")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "terminal status") && strings.Contains(err.Error(), "cancel") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected terminal cancellation-safety error, got: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_RejectsTerminalPublisherOnCancellation(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, `if: ${{ !cancelled() }}`, `if: ${{ always() }}`, 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("cancelled aggregate must not publish a terminal status")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "terminal status") && strings.Contains(err.Error(), "cancel") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected terminal cancellation-safety error, got: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_RejectsMalformedCancellationConditions(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing opening delimiter":  `!cancelled() }}`,
		"missing closing delimiter":  `${{ !cancelled()`,
		"extra opening delimiter":    `${{{ !cancelled() }}`,
		"extra closing delimiter":    `${{ !cancelled() }}}`,
		"empty expression":           `${{}}`,
		"empty condition":            ``,
		"trailing text":              `${{ !cancelled() }} trailing`,
		"second expression":          `${{ !cancelled() }} ${{ success() }}`,
		"space after unary operator": `${{ ! cancelled() }}`,
		"space before call":          `${{ !cancelled () }}`,
		"always expression":          `${{ always() }}`,
		"success expression":         `${{ success() }}`,
		"compound expression":        `${{ !cancelled() && always() }}`,
	}
	for name, condition := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := strings.Replace(
				trustedRequiredWorkflow,
				`        if: ${{ !cancelled() }}`,
				"        if: '"+condition+"'",
				1,
			)
			errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
			if len(errs) == 0 {
				t.Fatalf("malformed cancellation condition %q must be rejected", condition)
			}
			found := false
			for _, err := range errs {
				if strings.Contains(err.Error(), "terminal status") && strings.Contains(err.Error(), "cancel") {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected terminal cancellation-safety error, got: %v", errs)
			}
		})
	}
}

func TestCheckRequiredStatusWorkflows_RejectsExpressionsInShellScripts(t *testing.T) {
	t.Parallel()

	body := strings.Replace(
		trustedRequiredWorkflow,
		"run: go run ./cmd/ci-gates await",
		`run: go run ./cmd/ci-gates await --pr "${{ steps.pr.outputs.number }}"`,
		1,
	)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("trusted publisher must pass GitHub expressions through env before shell use")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "GitHub expression") && strings.Contains(err.Error(), "env") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected expression-in-shell error, got: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_RequiresInProgressInvalidation(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, "types: [in_progress, completed]", "types: [completed]", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("publisher must invalidate stale success when a blocking workflow starts")
	}
}

func TestCheckRequiredStatusWorkflows_RequiresSerializedPerHeadPublisher(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"running publisher cancellation": strings.Replace(
			trustedRequiredWorkflow,
			"cancel-in-progress: false",
			"cancel-in-progress: true",
			1,
		),
		"conditional publisher cancellation": strings.Replace(
			trustedRequiredWorkflow,
			"cancel-in-progress: false",
			"cancel-in-progress: ${{ github.event_name == 'workflow_run' }}",
			1,
		),
		"missing per-head concurrency": strings.Replace(
			trustedRequiredWorkflow,
			"concurrency:\n  group: required-gates-${{ github.event.workflow_run.head_sha || github.ref }}\n  cancel-in-progress: false\n",
			"",
			1,
		),
		"per-run concurrency group": strings.Replace(
			trustedRequiredWorkflow,
			"group: required-gates-${{ github.event.workflow_run.head_sha || github.ref }}",
			"group: required-gates-${{ github.run_id }}",
			1,
		),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
			if len(errs) == 0 {
				t.Fatal("trusted publisher must serialize per-head runs without cancelling the active writer")
			}
			if !strings.Contains(errs[0].Error(), "concurrency") {
				t.Fatalf("expected concurrency error, got: %v", errs)
			}
		})
	}
}

func TestCheckRequiredStatusWorkflows_RequiresPendingBeforeSetup(t *testing.T) {
	t.Parallel()

	body := strings.Replace(
		trustedRequiredWorkflow,
		"    steps:\n      - name: Publish pending",
		"    steps:\n      - name: Setup before invalidation\n        run: echo setup\n      - name: Publish pending",
		1,
	)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("pending invalidation must run before setup")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "pending") && strings.Contains(err.Error(), "first step") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected first-step pending error, got: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_RequiresUnconditionalPendingStatus(t *testing.T) {
	t.Parallel()

	body := strings.Replace(
		trustedRequiredWorkflow,
		"      - name: Publish pending\n",
		"      - name: Publish pending\n        if: ${{ false }}\n",
		1,
	)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("pending invalidation must execute unconditionally")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "pending") && strings.Contains(err.Error(), "unconditional") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unconditional pending error, got: %v", errs)
	}
}

func TestCheckRequiredStatusWorkflows_BindsEachStatusPublisherContext(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"pending publisher": strings.Replace(
			trustedRequiredWorkflow,
			"state=pending -f context=required-gates-complete",
			"state=pending -f context=unrelated-context",
			1,
		),
		"terminal publisher": strings.Replace(
			trustedRequiredWorkflow,
			"state=failure -f context=required-gates-complete",
			"state=failure -f context=unrelated-context",
			1,
		),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
			if len(errs) == 0 {
				t.Fatal("each status publisher must target the required context")
			}
			found := false
			for _, err := range errs {
				if strings.Contains(err.Error(), "status") && strings.Contains(err.Error(), "required context") {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected required-context error, got: %v", errs)
			}
		})
	}
}

func TestCheckRequiredStatusWorkflows_RequiresEveryBlockingWorkflowSource(t *testing.T) {
	t.Parallel()

	root := writeRequiredWorkflowFixture(t, trustedRequiredWorkflow)
	path := filepath.Join(root, ".github", "workflows", "security.yml")
	if err := os.WriteFile(path, []byte("name: Security Scan\non: pull_request\njobs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := requiredWorkflowRegistry()
	reg.Gates = []Gate{{
		ID:       "security",
		Blocking: true,
		CI:       CI{Workflow: "security.yml", Job: "scan"},
	}}

	errs := checkRequiredStatusWorkflows(root, reg)
	if len(errs) == 0 {
		t.Fatal("every blocking gate workflow must wake the trusted publisher")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "Security Scan") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing Security Scan source error, got: %v", errs)
	}
}

func TestWorkflowTriggerKeys_Sequence(t *testing.T) {
	t.Parallel()

	keys, err := workflowTriggerKeys([]byte("name: Trigger Test\non: [push, workflow_run, pull_request_target]\njobs: {}\n"))
	if err != nil {
		t.Fatalf("parse sequence triggers: %v", err)
	}
	for _, want := range []string{"push", "workflow_run", "pull_request_target"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("sequence triggers missing %q: %v", want, keys)
		}
	}
}

func TestWorkflowTriggerKeys_RejectsAlias(t *testing.T) {
	t.Parallel()

	_, err := workflowTriggerKeys([]byte("events: &events [push, workflow_run]\non: *events\njobs: {}\n"))
	if err == nil {
		t.Fatal("alias trigger configuration must be rejected")
	}
}
