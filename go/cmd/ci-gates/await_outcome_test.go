// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The publisher in .github/workflows/required-gates.yml defaults to `failure`
// for every non-success await outcome, so "a required gate went red", "the
// gates are still running", and "the aggregation itself broke" all land on the
// head SHA as the same red status (#6075).
//
// That makes the status uninformative in the worst possible place: it is
// branch protection's summary of every other gate, and the correct reaction to
// a red became "wait and look again", which is indistinguishable from how you
// would treat a flake. These pin the classification the publisher needs to
// tell the three apart.

// TestAwaitOutcomeForGenuineGateFailure keeps the case that MUST stay red.
// Weakening this is the false-green risk in the whole change: if a real gate
// failure is ever classified as "still running", branch protection stops
// blocking on it.
func TestAwaitOutcomeForGenuineGateFailure(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: go-core (Build Test)", errGateFailed)
	got := classifyAwaitOutcome(err)
	if got != awaitOutcomeGateFailed {
		t.Fatalf("classifyAwaitOutcome(gate failure) = %v, want %v", got, awaitOutcomeGateFailed)
	}
	if got.exitCode() != awaitExitGateFailed {
		t.Fatalf("gate failure exit code = %d, want %d", got.exitCode(), awaitExitGateFailed)
	}
}

// TestAwaitOutcomeForStillRunning is the defect: a timeout while gates are
// still pending is not a gate result and must never publish failure.
func TestAwaitOutcomeForStillRunning(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: timed out waiting for (go-race (2)): context deadline exceeded", errStillRunning)
	got := classifyAwaitOutcome(err)
	if got != awaitOutcomeStillRunning {
		t.Fatalf("classifyAwaitOutcome(timeout) = %v, want %v", got, awaitOutcomeStillRunning)
	}
	if got.exitCode() != awaitExitStillRunning {
		t.Fatalf("still-running exit code = %d, want %d", got.exitCode(), awaitExitStillRunning)
	}
}

// TestAwaitOutcomeForAggregationBreakage covers the third case: the publisher
// could not reach a verdict at all. That is a publisher problem, not a gate
// result, and reporting it as `failure` sends people hunting for a red gate
// that does not exist.
func TestAwaitOutcomeForAggregationBreakage(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		errors.New("read PR #123 checks: gh: HTTP 403"),
		errors.New("load registry: open specs/ci-gates.v1.yaml: no such file or directory"),
	} {
		got := classifyAwaitOutcome(err)
		if got != awaitOutcomeBroken {
			t.Errorf("classifyAwaitOutcome(%q) = %v, want %v", err, got, awaitOutcomeBroken)
		}
		if got.exitCode() != awaitExitBroken {
			t.Errorf("broken exit code = %d, want %d", got.exitCode(), awaitExitBroken)
		}
	}
}

// TestAwaitOutcomeForSuccess pins the happy path and its exit code, so a
// refactor cannot make success indistinguishable from "still running".
func TestAwaitOutcomeForSuccess(t *testing.T) {
	t.Parallel()

	got := classifyAwaitOutcome(nil)
	if got != awaitOutcomePassed {
		t.Fatalf("classifyAwaitOutcome(nil) = %v, want %v", got, awaitOutcomePassed)
	}
	if got.exitCode() != 0 {
		t.Fatalf("success exit code = %d, want 0", got.exitCode())
	}
}

// TestAwaitOutcomeExitCodesAreDistinct is what makes the workflow mapping
// possible at all: the publisher branches on the numeric code, so two
// outcomes sharing one code silently collapses two states into one.
func TestAwaitOutcomeExitCodesAreDistinct(t *testing.T) {
	t.Parallel()

	seen := map[int]awaitOutcome{}
	for _, o := range []awaitOutcome{
		awaitOutcomePassed,
		awaitOutcomeGateFailed,
		awaitOutcomeStillRunning,
		awaitOutcomeBroken,
	} {
		if prev, dup := seen[o.exitCode()]; dup {
			t.Fatalf("outcomes %v and %v share exit code %d", prev, o, o.exitCode())
		}
		seen[o.exitCode()] = o
	}
}

// TestAwaitOutcomeIgnoresLookalikeText is the reason the sentinels are wrapped
// rather than matched on message text. An error that merely READS like a gate
// failure, but carries no sentinel, must classify as `broken` -- the publisher
// does not know what happened, and asserting "a gate failed" on that basis is
// the overclaim this change removes. It also means a future reword of an error
// string cannot silently demote a real gate failure to "still running", which
// is the direction that would stop blocking merges.
func TestAwaitOutcomeIgnoresLookalikeText(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		errors.New("selected blocking checks failed: go-core (Build Test)"),
		errors.New("timed out waiting for selected blocking checks (go-race (2))"),
	} {
		if got := classifyAwaitOutcome(err); got != awaitOutcomeBroken {
			t.Errorf("classifyAwaitOutcome(unwrapped %q) = %v, want %v — classification must be structural, not textual", err, got, awaitOutcomeBroken)
		}
	}
}

// TestAwaitTimeoutWithPendingGatesIsNotAGateFailure drives the real await loop
// rather than a hand-built error string, so the classification is proven
// against the message the code actually emits. A string-only test would keep
// passing if the wording drifted.
func TestAwaitTimeoutWithPendingGatesIsNotAGateFailure(t *testing.T) {
	t.Parallel()

	runner := &stubRunner{output: []byte(`[
		{"name":"go-core","state":"SUCCESS","bucket":"pass","workflow":"Build Test","event":"pull_request"},
		{"name":"go-race (2)","state":"PENDING","bucket":"pending","workflow":"Build Test","event":"pull_request"}
	]`)}
	required := []resolvedRequiredGate{
		{WorkflowName: "Build Test", Job: "go-core", GateIDs: []string{"go-core"}},
		{WorkflowName: "Build Test", Job: "go-race (2)", GateIDs: []string{"go-race"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 1, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("await must not report success while a selected gate is still pending")
	}
	if got := classifyAwaitOutcome(err); got != awaitOutcomeStillRunning {
		t.Fatalf("classifyAwaitOutcome(%q) = %v, want %v — a pending gate is not a failed gate", err, got, awaitOutcomeStillRunning)
	}
	if !strings.Contains(err.Error(), "go-race (2)") {
		t.Errorf("timeout message should name the pending gate, got %q", err.Error())
	}
}

// io_Discard avoids importing io just for a sink in this file.
type io_Discard struct{}

func (io_Discard) Write(p []byte) (int, error) { return len(p), nil }

// stubRunner returns one canned `gh pr checks` payload for every call, so the
// await loop keeps seeing the same pending gate until its context expires.
type stubRunner struct{ output []byte }

func (s *stubRunner) Run(context.Context, ...string) ([]byte, error) { return s.output, nil }
