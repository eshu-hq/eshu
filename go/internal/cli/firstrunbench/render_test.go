// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrunbench

import (
	"strings"
	"testing"
	"time"
)

// TestMarkerCoversEveryStatus pins the stable ASCII markers operators grep
// for, including the default for an unscored (empty) status.
func TestMarkerCoversEveryStatus(t *testing.T) {
	cases := map[CriterionStatus]string{
		CriterionPass:        "[ok]",
		CriterionFail:        "[!!]",
		CriterionNotMeasured: "[--]",
		CriterionStatus(""):  "[--]",
	}
	for status, want := range cases {
		if got := Marker(status); got != want {
			t.Fatalf("Marker(%q) = %q, want %q", status, got, want)
		}
	}
}

// TestRenderVerdictSubstitutesEmptyPath pins the local copy of the empty-value
// placeholder: an empty path renders as <repo> so the scorecard line stays
// copy-pasteable, matching the helper in go/cmd/eshu/first_run.go.
func TestRenderVerdictSubstitutesEmptyPath(t *testing.T) {
	env := completeEnvelope()
	verdict := Evaluate(env, Measurements{Path: "  ", Elapsed: 2 * time.Second})

	var b strings.Builder
	RenderVerdict(&b, verdict)
	out := b.String()

	if !strings.Contains(out, "path : <repo>") {
		t.Fatalf("rendered scorecard missing 'path : <repo>' placeholder; got:\n%s", out)
	}
	if !strings.Contains(out, "First-answer benchmark PASSED") {
		t.Fatalf("rendered scorecard missing PASS header; got:\n%s", out)
	}
	if !strings.Contains(out, "[ok] * first_answer_returned") {
		t.Fatalf("rendered scorecard missing required first-answer row; got:\n%s", out)
	}
}

// TestRenderVerdictFailedRun proves the FAILED header and the [!!] marker on
// the failing required row survive rendering.
func TestRenderVerdictFailedRun(t *testing.T) {
	env := completeEnvelope()
	env.Data.QueryAnswered = false
	env.Data.QuerySummary = ""
	verdict := Evaluate(env, Measurements{Path: "local_binary"})

	var b strings.Builder
	RenderVerdict(&b, verdict)
	out := b.String()

	if !strings.Contains(out, "First-answer benchmark FAILED") {
		t.Fatalf("rendered scorecard missing FAILED header; got:\n%s", out)
	}
	if !strings.Contains(out, "[!!] * first_answer_returned") {
		t.Fatalf("rendered scorecard missing failed required row; got:\n%s", out)
	}
}
