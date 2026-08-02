// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

type scriptedGHResult struct {
	output string
	err    error
}

type scriptedGHRunner struct {
	results []scriptedGHResult
	calls   [][]string
}

func (r *scriptedGHRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(r.results) == 0 {
		return nil, errors.New("unexpected gh invocation")
	}
	result := r.results[0]
	r.results = r.results[1:]
	return []byte(result.output), result.err
}

func TestResolveRequiredGateWorkflows(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workflowDir, "static.yml"),
		[]byte("name: Static Contract Gates\non: pull_request\njobs: {}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRequiredGateWorkflows(root, []cigates.RequiredGate{{
		Workflow: "static.yml",
		Job:      "Verify OpenAPI gate",
		GateIDs:  []string{"openapi-surface"},
	}})
	if err != nil {
		t.Fatalf("resolveRequiredGateWorkflows returned error: %v", err)
	}
	want := []resolvedRequiredGate{{
		WorkflowFile: "static.yml",
		WorkflowName: "Static Contract Gates",
		Job:          "Verify OpenAPI gate",
		GateIDs:      []string{"openapi-surface"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved required gates = %#v; want %#v", got, want)
	}
}

func TestEvaluateRequiredChecks_MatrixRequiresEveryDeclaredLeg(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{
		{
			WorkflowName: "End-to-end Tests",
			Job:          "test (nornicdb)",
			GateIDs:      []string{"e2e-tests"},
		},
		{
			WorkflowName: "End-to-end Tests",
			Job:          "test (neo4j)",
			GateIDs:      []string{"e2e-tests"},
		},
	}
	checks := []checkRollup{
		{Name: "test (nornicdb)", Workflow: "End-to-end Tests", Event: "pull_request", Bucket: "pass"},
		{Name: "test (neo4j)", Workflow: "End-to-end Tests", Event: "pull_request", Bucket: "pending"},
	}

	got := evaluateRequiredChecks(required, checks)
	if len(got.Failed) != 0 {
		t.Fatalf("failed = %v; want none", got.Failed)
	}
	if len(got.Pending) != 1 || got.Pending[0].Job != "test (neo4j)" {
		t.Fatalf("pending = %v; want matrix job pending", got.Pending)
	}
}

func TestEvaluateRequiredChecks_SelectedSkipFailsClosed(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Security Scan",
		Job:          "gosec (Go static analysis)",
		GateIDs:      []string{"gosec-changed"},
	}}
	checks := []checkRollup{{
		Name:     "gosec (Go static analysis)",
		Workflow: "Security Scan",
		Event:    "pull_request",
		Bucket:   "skipping",
		State:    "SKIPPED",
	}}

	got := evaluateRequiredChecks(required, checks)
	if len(got.Pending) != 0 {
		t.Fatalf("pending = %v; want none", got.Pending)
	}
	if len(got.Failed) != 1 {
		t.Fatalf("failed = %v; want selected skipped job to fail", got.Failed)
	}
}

func TestEvaluateRequiredChecks_MissingCheckRemainsPending(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Frontend",
		Job:          "Console (apps/console)",
		GateIDs:      []string{"console-e2e"},
	}}

	got := evaluateRequiredChecks(required, nil)
	if len(got.Failed) != 0 || len(got.Pending) != 1 {
		t.Fatalf("evaluation = %#v; want one pending missing check", got)
	}
}

func TestEvaluateRequiredChecks_FailureNamesAllRepresentedGates(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Build Test",
		Job:          "verify-contracts",
		GateIDs:      []string{"license-header", "package-docs"},
	}}
	checks := []checkRollup{{
		Name:     "verify-contracts",
		Workflow: "Build Test",
		Event:    "pull_request",
		Bucket:   "fail",
		State:    "FAILURE",
	}}

	got := evaluateRequiredChecks(required, checks)
	if len(got.Failed) != 1 {
		t.Fatalf("failed = %v; want one", got.Failed)
	}
	if got.Failed[0].GateIDs[0] != "license-header" || got.Failed[0].GateIDs[1] != "package-docs" {
		t.Fatalf("failed gate ids = %v", got.Failed[0].GateIDs)
	}
}

func TestEvaluateRequiredChecks_DoesNotAcceptPushCheckForPR(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Build Test",
		Job:          "verify-contracts",
		GateIDs:      []string{"license-header"},
	}}
	checks := []checkRollup{{
		Name:     "verify-contracts",
		Workflow: "Build Test",
		Event:    "push",
		Bucket:   "pass",
	}}

	got := evaluateRequiredChecks(required, checks)
	if len(got.Failed) != 0 || len(got.Pending) != 1 {
		t.Fatalf("push check must not satisfy a pull-request gate: %#v", got)
	}
}

func TestChangedPathsForPR_UsesPaginatedFilesAPI(t *testing.T) {
	t.Parallel()

	runner := &scriptedGHRunner{results: []scriptedGHResult{
		{output: `[[{"filename":"go/new.go","previous_filename":"go/old.go"},{"filename":"docs/index.md"}]]`},
		{output: "2\n"},
	}}
	got, err := changedPathsForPR(context.Background(), runner, "eshu-hq/eshu", 42)
	if err != nil {
		t.Fatalf("changedPathsForPR returned error: %v", err)
	}
	want := []string{"go/new.go", "go/old.go", "docs/index.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed paths = %v; want %v", got, want)
	}
	if len(runner.calls) != 2 || !reflect.DeepEqual(runner.calls[0], []string{
		"api", "--paginate", "--slurp", "repos/eshu-hq/eshu/pulls/42/files",
	}) {
		t.Fatalf("gh args = %v", runner.calls)
	}
	if !reflect.DeepEqual(runner.calls[1], []string{
		"pr", "view", "42", "--repo", "eshu-hq/eshu", "--json", "changedFiles", "--jq", ".changedFiles",
	}) {
		t.Fatalf("changed-file count args = %v", runner.calls[1])
	}
}

func TestChangedPathsForPR_FailsClosedOnTruncatedFilesResponse(t *testing.T) {
	t.Parallel()

	runner := &scriptedGHRunner{results: []scriptedGHResult{
		{output: `[[{"filename":"go/a.go"}]]`},
		{output: "3001\n"},
	}}
	_, err := changedPathsForPR(context.Background(), runner, "eshu-hq/eshu", 42)
	if err == nil {
		t.Fatal("truncated pull-files response must fail closed")
	}
	if !strings.Contains(err.Error(), "1 of 3001") {
		t.Fatalf("truncation error should report returned and expected counts: %v", err)
	}
}

func TestAwaitPRRequiredChecks_AcceptsPendingGHExitThenPasses(t *testing.T) {
	t.Parallel()

	runner := &scriptedGHRunner{results: []scriptedGHResult{
		{
			output: `[{"name":"verify-contracts","state":"IN_PROGRESS","bucket":"pending","workflow":"Build Test","event":"pull_request"}]`,
			err:    errors.New("exit status 8"),
		},
		{
			output: `[{"name":"verify-contracts","state":"COMPLETED","bucket":"pass","workflow":"Build Test","event":"pull_request"}]`,
		},
	}}
	required := []resolvedRequiredGate{{
		WorkflowName: "Build Test",
		Job:          "verify-contracts",
		GateIDs:      []string{"license-header"},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 42, required, time.Millisecond, io.Discard); err != nil {
		t.Fatalf("awaitPRRequiredChecks returned error: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("gh calls = %d; want 2", len(runner.calls))
	}
	for _, call := range runner.calls {
		want := []string{
			"pr", "checks", "42", "--repo", "eshu-hq/eshu",
			"--json", "name,state,bucket,workflow,event",
		}
		if !reflect.DeepEqual(call, want) {
			t.Fatalf("gh args = %v; want supported check-rollup fields %v", call, want)
		}
	}
}

func TestAwaitPRRequiredChecks_FailsClosedOnRedCheck(t *testing.T) {
	t.Parallel()

	runner := &scriptedGHRunner{results: []scriptedGHResult{{
		output: `[{"name":"verify-contracts","state":"COMPLETED","bucket":"fail","workflow":"Build Test","event":"pull_request"}]`,
		err:    errors.New("exit status 1"),
	}}}
	required := []resolvedRequiredGate{{
		WorkflowName: "Build Test",
		Job:          "verify-contracts",
		GateIDs:      []string{"license-header"},
	}}

	err := awaitPRRequiredChecks(
		context.Background(),
		runner,
		"eshu-hq/eshu",
		42,
		required,
		time.Millisecond,
		io.Discard,
	)
	if err == nil {
		t.Fatal("red selected blocking check should fail the aggregate")
	}
}
