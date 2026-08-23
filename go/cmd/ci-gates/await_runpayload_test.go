// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Why this file exists (#6189, third round).
//
// Every other test of the run lookup builds its own page envelope, so all of
// them prove is that the decode agrees with a shape this repository invented.
// The shape that actually has to hold is GitHub's, wrapped by `gh api
// --paginate --slurp`, and if either changes the SKIPPED carve-out goes inert
// without a single test noticing: the decode fails, the conclusions map goes
// nil, and every cancellation-skipped gate reverts to publishing `failure`.
// That degradation is fail-closed, which is why nothing red would appear.
//
// These fixtures are captured responses, not hand-built ones.
//
//	gh api --paginate --slurp \
//	  'repos/eshu-hq/eshu/actions/runs?per_page=100&head_sha=<head>'
//
// captured 2026-08-23 against eshu-hq/eshu:
//
//   - workflow_runs_cancelled_head.json -- head
//     b3b7d62cea9cab706db6d93e1524c40fe7e59c81, the #6189 transcript shape: a
//     head whose pull-request runs were mass-cancelled. 3 of its 20 runs kept.
//   - workflow_runs_inflight_head.json -- head
//     37bec5ece5ab4c8fc6ac33359677136d964e93ae, captured while one run had not
//     finished, so it carries a real `"conclusion": null`. 2 of its 15 runs
//     kept.
//
// The only edits to either file are those two deletions and the replacement of
// the commit author/committer email with redacted@example.invalid. The page
// envelope, and every kept run object, are byte-for-byte as GitHub and gh
// returned them -- including the ~30 fields this command does not read, since
// "unknown fields are ignored" is part of what is being pinned.

// runPayloadFixture reads a captured `gh api --paginate --slurp` response.
func runPayloadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read captured run payload: %v", err)
	}
	return raw
}

// TestWorkflowRunConclusionsDecodesACapturedGhPayload is the decode contract
// against real bytes rather than against this repository's model of them.
func TestWorkflowRunConclusionsDecodesACapturedGhPayload(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		fixture   string
		cancelled map[string]bool
		inFlight  map[string]bool
		absent    []string
	}{
		{
			name:    "mass-cancelled head",
			fixture: "workflow_runs_cancelled_head.json",
			// The capture's own values: Build Test was cancelled, Product
			// Claim Ledger finished first and succeeded.
			cancelled: map[string]bool{"Build Test": true, "Product Claim Ledger": false},
			inFlight:  map[string]bool{"Build Test": false, "Product Claim Ledger": false},
			// `PR #6019` is a real run on the same head with event
			// `dynamic`. It does not own the pull-request checks the
			// aggregate evaluates, and dropping the event filter would let a
			// run like it decide a gate's verdict.
			absent: []string{"PR #6019"},
		},
		{
			name:    "head with a run still executing",
			fixture: "workflow_runs_inflight_head.json",
			// GitHub sends `"conclusion": null` for a run that has not
			// finished. This is the state the re-run repair passes through,
			// and the fixture proves GitHub really emits null there rather
			// than omitting the field or sending an empty string.
			cancelled: map[string]bool{"Benchmarks": false, "Build Test": false},
			inFlight:  map[string]bool{"Benchmarks": true, "Build Test": false},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runner := &routedRunner{runs: runPayloadFixture(t, tc.fixture)}
			got, err := workflowRunConclusions(context.Background(), runner, "eshu-hq/eshu", headSHAFixture)
			if err != nil {
				t.Fatalf("a captured gh --paginate --slurp response must decode: %v", err)
			}
			for workflow, want := range tc.cancelled {
				if got.cancelled(workflow) != want {
					t.Errorf("cancelled(%q) = %v, want %v (decoded %#v)", workflow, got.cancelled(workflow), want, got)
				}
			}
			for workflow, want := range tc.inFlight {
				if got.inFlight(workflow) != want {
					t.Errorf("inFlight(%q) = %v, want %v (decoded %#v)", workflow, got.inFlight(workflow), want, got)
				}
			}
			for _, workflow := range tc.absent {
				if _, ok := got[workflow]; ok {
					t.Errorf("%q is not a pull-request run and must not appear in the conclusions (decoded %#v)", workflow, got)
				}
			}
		})
	}
}

// TestWorkflowRunConclusionsRejectsTheUnslurpedShape is the other half of the
// same contract, and it is what makes the `--paginate --slurp` argv assertion
// mean something. Without those flags gh returns the endpoint's own single
// `{"workflow_runs": [...]}` object instead of an array of pages. The fixture
// is that object, lifted unchanged out of the captured array, so the two tests
// differ only by the wrapper the flags produce.
func TestWorkflowRunConclusionsRejectsTheUnslurpedShape(t *testing.T) {
	t.Parallel()

	page := runPayloadFixture(t, "workflow_runs_cancelled_head.json")
	unslurped := unwrapFirstPage(t, page)

	runner := &routedRunner{runs: unslurped}
	got, err := workflowRunConclusions(context.Background(), runner, "eshu-hq/eshu", headSHAFixture)
	if err == nil {
		t.Fatalf("the un-paginated single-object shape decoded to %#v; it must be a hard error, "+
			"because a silent empty map turns every cancellation-skipped gate back into a published failure", got)
	}
	if got != nil {
		t.Errorf("a failed decode must return no conclusions, got %#v", got)
	}
}

// unwrapFirstPage returns the first element of a `--slurp` page array: exactly
// the bytes `gh api` returns for the same endpoint without `--paginate`.
func unwrapFirstPage(t *testing.T, slurped []byte) []byte {
	t.Helper()
	var pages []json.RawMessage
	if err := json.Unmarshal(slurped, &pages); err != nil {
		t.Fatalf("captured payload is not a page array: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("captured payload has %d pages; this helper assumes the single-page capture", len(pages))
	}
	return pages[0]
}
