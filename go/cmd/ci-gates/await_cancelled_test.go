// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Why this file exists (#6189).
//
// A CANCELLED dependency gate was classified as a FAILED gate, so the
// aggregate published `state=failure` with description "A required gate
// failed" when no gate failed. Cancellations happen for reasons unrelated to
// a pull request's content, so a PR that catches one goes red regardless of
// whether anything is wrong with it -- which trains people to read a red
// `required-gates-complete` as noise, and that is the expensive part.
//
// These tests assert the PUBLISHED STATE AND DESCRIPTION, not just the
// process exit code. The exit code is only half the contract: the other half
// is the `case "${AGGREGATE_CODE}"` mapping in
// .github/workflows/required-gates.yml, and an exit-code-only test would pass
// while the workflow still published "A required gate failed" -- an
// expected-fail passing for the wrong reason.

// publishedRequiredStatus evaluates the real publisher's AGGREGATE_CODE case
// block from .github/workflows/required-gates.yml for one exit code and
// returns the (state, description) it would post, plus whether it posts at
// all. The block is executed by bash rather than re-implemented in Go, so the
// test reads the same shell the runner does; re-implementing the mapping here
// would let the two drift and still agree with each other.
//
// Only the AGGREGATE_CODE mapping is evaluated. The step's outer
// PENDING_OUTCOME guard is a separate contract (#5980) and is asserted by
// checkRequiredStatusWorkflows in internal/cigates.
func publishedRequiredStatus(t *testing.T, code int) (state, description string, published bool) {
	t.Helper()

	workflow := filepath.Join(repoRoot(t), ".github", "workflows", "required-gates.yml")
	raw, err := os.ReadFile(workflow) // #nosec G304 -- fixed repo-relative test fixture path
	if err != nil {
		t.Fatalf("read %s: %v", workflow, err)
	}
	const openMarker = `case "${AGGREGATE_CODE}" in`
	const closeMarker = "esac"
	start := bytes.Index(raw, []byte(openMarker))
	if start < 0 {
		t.Fatalf("required-gates.yml has no %q block; the publisher contract moved", openMarker)
	}
	rest := string(raw[start:])
	end := strings.Index(rest, closeMarker)
	if end < 0 {
		t.Fatalf("required-gates.yml %q block is unterminated", openMarker)
	}
	block := rest[:end+len(closeMarker)]

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not available to evaluate the publisher case block: %v", err)
	}
	script := "set -u\nAGGREGATE_CODE=" + strconv.Itoa(code) + "\nstate=\ndescription=\n" + block +
		"\nprintf 'PUBLISH\\t%s\\t%s\\n' \"${state}\" \"${description}\"\n"
	out, err := exec.Command(bash, "-c", script).CombinedOutput() // #nosec G204 -- script is built from a committed workflow file, not input
	if err != nil {
		t.Fatalf("evaluate publisher case block for code %d: %v\n%s", code, err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "PUBLISH\t") {
			continue
		}
		fields := strings.SplitN(strings.TrimPrefix(line, "PUBLISH\t"), "\t", 2)
		if len(fields) != 2 {
			t.Fatalf("malformed publisher result %q", line)
		}
		return fields[0], fields[1], true
	}
	return "", "", false
}

// cancelledTranscriptChecks reproduces the eleven-gate rollup from the #6189
// report: every selected blocking gate CANCELLED, none FAILURE.
func cancelledTranscriptChecks() ([]resolvedRequiredGate, []checkRollup) {
	rows := []struct{ workflow, job, gate string }{
		{"Static Contract Gates", "Verify route coverage gate", "route-coverage"},
		{"Static Contract Gates", "Verify evidence continuity gate", "evidence-continuity"},
		{"Static Contract Gates", "Verify telemetry coverage gate", "telemetry-coverage"},
		{"Static Contract Gates", "Verify generate-operator-dashboard gate", "operator-dashboard"},
		{"Build Test", "docs-helm-hygiene", "docs-catalog-metadata"},
		{"Static Contract Gates", "Verify docs-refs gate", "docs-refs"},
		{"Static Contract Gates", "Verify docs CLI/env refs gate", "docs-cli-env-refs"},
		{"Static Contract Gates", "Verify query-doc-commit-refs gate", "query-doc-commit-refs"},
		{"Static Contract Gates", "Verify doc-citations gate", "doc-citations"},
		{"Static Contract Gates", "Verify measurement-citations gate", "measurement-citations"},
		{"Frontend", "MCP-identity auth E2E", "auth-mcp-e2e"},
	}
	required := make([]resolvedRequiredGate, 0, len(rows))
	checks := make([]checkRollup, 0, len(rows))
	for _, row := range rows {
		required = append(required, resolvedRequiredGate{
			WorkflowName: row.workflow,
			Job:          row.job,
			GateIDs:      []string{row.gate},
		})
		checks = append(checks, checkRollup{
			Name:     row.job,
			Workflow: row.workflow,
			Event:    "pull_request",
			// gh >= 2.94 buckets CANCELLED as "cancel"; older gh bucketed it
			// as "fail". The transcript in #6189 shows state=CANCELLED, which
			// is stable across both.
			Bucket: "cancel",
			State:  "CANCELLED",
		})
	}
	return required, checks
}

func rollupJSON(t *testing.T, checks []checkRollup) []byte {
	t.Helper()
	raw, err := json.Marshal(checks)
	if err != nil {
		t.Fatalf("marshal check rollup: %v", err)
	}
	return raw
}

// TestAwaitAllCancelledDependenciesDoNotPublishFailure is the #6189 defect.
// Eleven cancelled dependencies, zero failures, and the aggregate posted
// "A required gate failed" on the head SHA.
func TestAwaitAllCancelledDependenciesDoNotPublishFailure(t *testing.T) {
	t.Parallel()

	required, checks := cancelledTranscriptChecks()
	runner := &stubRunner{output: rollupJSON(t, checks)}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("cancelled dependencies must not be reported as a clean pass")
	}
	code := classifyAwaitOutcome(err).exitCode()
	state, description, published := publishedRequiredStatus(t, code)
	if !published {
		t.Fatalf("exit code %d publishes nothing; a cancelled dependency must stay visible, not leave the status pending forever", code)
	}
	if state == "failure" {
		t.Fatalf("cancelled dependencies published state=%q description=%q (exit %d); no gate failed", state, description, code)
	}
	if state != "error" {
		t.Fatalf("cancelled dependencies published state=%q description=%q (exit %d); want error", state, description, code)
	}
	if !strings.Contains(strings.ToLower(description), "cancel") {
		t.Fatalf("published description %q does not tell the operator a gate was cancelled", description)
	}
}

// TestAwaitCancelledPlusGenuineFailureStillPublishesFailure is the guard that
// matters more than the fix: turning real gate failures green would be far
// worse than the bug being fixed. A rollup carrying both cancellations and one
// genuine FAILURE must still publish `failure`.
func TestAwaitCancelledPlusGenuineFailureStillPublishesFailure(t *testing.T) {
	t.Parallel()

	required, checks := cancelledTranscriptChecks()
	required = append(required, resolvedRequiredGate{
		WorkflowName: "Build Test",
		Job:          "go-core",
		GateIDs:      []string{"go-core"},
	})
	checks = append(checks, checkRollup{
		Name:     "go-core",
		Workflow: "Build Test",
		Event:    "pull_request",
		Bucket:   "fail",
		State:    "FAILURE",
	})

	runner := &stubRunner{output: rollupJSON(t, checks)}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("a genuinely failed gate must fail the aggregate")
	}
	if !strings.Contains(err.Error(), "go-core") {
		t.Errorf("failure message must name the gate that actually failed, got %q", err.Error())
	}
	code := classifyAwaitOutcome(err).exitCode()
	state, description, published := publishedRequiredStatus(t, code)
	if !published || state != "failure" {
		t.Fatalf("a real gate failure alongside cancellations published state=%q description=%q published=%v; want failure", state, description, published)
	}
}

// TestEvaluateRequiredChecksSeparatesCancelledFromFailed pins the evaluation
// itself, so the classification cannot be re-collapsed inside the await loop.
func TestEvaluateRequiredChecksSeparatesCancelledFromFailed(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Verify Agent Hygiene",
		Job:          "verify-agent-hygiene",
		GateIDs:      []string{"agent-hygiene"},
	}}
	checks := []checkRollup{{
		Name:     "verify-agent-hygiene",
		Workflow: "Verify Agent Hygiene",
		Event:    "pull_request",
		Bucket:   "cancel",
		State:    "CANCELLED",
	}}

	got := evaluateRequiredChecks(required, checks)
	if len(got.Failed) != 0 {
		t.Fatalf("cancelled gate classified as failed: %#v", got.Failed)
	}
	if len(got.Pending) != 0 {
		t.Fatalf("cancelled gate is terminal and must not be reported pending: %#v", got.Pending)
	}
}

// TestEvaluateRequiredChecksTreatsOldGHCancelBucketAsCancelled covers gh
// versions that bucket CANCELLED as "fail" rather than "cancel". The runner's
// gh version is not pinned by this repo, and the #6189 transcript's findings
// carried state=CANCELLED, which is the signal stable across both.
func TestEvaluateRequiredChecksTreatsOldGHCancelBucketAsCancelled(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Frontend",
		Job:          "MCP-identity auth E2E",
		GateIDs:      []string{"auth-mcp-e2e"},
	}}
	checks := []checkRollup{{
		Name:     "MCP-identity auth E2E",
		Workflow: "Frontend",
		Event:    "pull_request",
		Bucket:   "fail",
		State:    "CANCELLED",
	}}

	got := evaluateRequiredChecks(required, checks)
	if len(got.Failed) != 0 {
		t.Fatalf("cancelled gate reported under the older gh bucket classified as failed: %#v", got.Failed)
	}
}

// TestEvaluateRequiredChecksTreatsCancelBucketWithoutCancelledStateAsCancelled
// covers the other half of the detection. Every other cancellation fixture in
// this file carries state=CANCELLED, so deleting the bucket check -- the half
// that survives a gh which renames or drops that field -- left them all green
// while the README's claim, that a gh upgrade or downgrade on the runner
// cannot silently restore the overclaim, quietly stopped being true.
//
// The state field is left empty rather than set to some other conclusion: the
// point is a gh that files the row in the "cancel" bucket while reporting a
// state string this code does not recognise, and an empty state is the
// clearest way to say that only the bucket carries the signal here.
func TestEvaluateRequiredChecksTreatsCancelBucketWithoutCancelledStateAsCancelled(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Static Contract Gates",
		Job:          "Verify docs-refs gate",
		GateIDs:      []string{"docs-refs"},
	}}
	checks := []checkRollup{{
		Name:     "Verify docs-refs gate",
		Workflow: "Static Contract Gates",
		Event:    "pull_request",
		Bucket:   "cancel",
		State:    "",
	}}

	got := evaluateRequiredChecks(required, checks)
	if len(got.Failed) != 0 {
		t.Fatalf("a gate bucketed cancel classified as failed: %#v", got.Failed)
	}
	if len(got.Pending) != 0 {
		t.Fatalf("a cancelled gate is terminal and must not be reported pending: %#v", got.Pending)
	}
	if len(got.Cancelled) != 1 {
		t.Fatalf("cancelled = %#v; bucket=cancel alone must be enough to detect a cancellation", got.Cancelled)
	}
}

// TestEvaluateRequiredChecksPrefersPendingOverCancelledWithinOneGate pins the
// precedence inside a single gate, which the two-gate test below cannot see:
// that one exercises the outer aggregation, where any pending gate keeps the
// wait alive regardless of what the other gates did.
//
// This ordering is defence rather than a property observable through today's
// reader -- `gh pr checks` de-duplicates by name/workflow/event, the same
// triple matchingChecks keys on, so a gate resolves to at most one rollup row
// (see the note on the precedence switch in await.go). Pinning it anyway costs
// one fixture and stops a silent inversion if that ever stops holding.
func TestEvaluateRequiredChecksPrefersPendingOverCancelledWithinOneGate(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Build Test",
		Job:          "go-core",
		GateIDs:      []string{"go-core"},
	}}
	checks := []checkRollup{
		{Name: "go-core", Workflow: "Build Test", Event: "pull_request", Bucket: "cancel", State: "CANCELLED"},
		{Name: "go-core", Workflow: "Build Test", Event: "pull_request", Bucket: "pending", State: "IN_PROGRESS"},
	}

	got := evaluateRequiredChecks(required, checks)
	if len(got.Failed) != 0 {
		t.Fatalf("neither leg failed, so nothing may be classified failed: %#v", got.Failed)
	}
	if len(got.Pending) != 1 || got.Pending[0].Job != "go-core" {
		t.Fatalf("pending = %#v; a leg still running may yet go red and must keep the wait alive", got.Pending)
	}
	if len(got.Cancelled) != 0 {
		t.Fatalf("cancelled = %#v; reporting a cancellation while a leg is still in flight understates the head", got.Cancelled)
	}
}

// TestEvaluateRequiredChecksKeepsWaitingWhileCancelledAndPendingCoexist keeps
// the honest verdict reachable. A gate still running may yet go genuinely red,
// so a cancellation alongside it must not short-circuit the wait -- otherwise
// the aggregate reports "cancelled" for a head that really did have a failing
// gate.
func TestEvaluateRequiredChecksKeepsWaitingWhileCancelledAndPendingCoexist(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{
		{WorkflowName: "Build Test", Job: "docs-helm-hygiene", GateIDs: []string{"docs-refs"}},
		{WorkflowName: "Build Test", Job: "go-core", GateIDs: []string{"go-core"}},
	}
	checks := []checkRollup{
		{Name: "docs-helm-hygiene", Workflow: "Build Test", Event: "pull_request", Bucket: "cancel", State: "CANCELLED"},
		{Name: "go-core", Workflow: "Build Test", Event: "pull_request", Bucket: "pending", State: "IN_PROGRESS"},
	}

	got := evaluateRequiredChecks(required, checks)
	if len(got.Failed) != 0 {
		t.Fatalf("cancelled gate classified as failed: %#v", got.Failed)
	}
	if len(got.Pending) != 1 || got.Pending[0].Job != "go-core" {
		t.Fatalf("pending = %#v; the still-running gate must keep the wait alive", got.Pending)
	}
}
