// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
package backendconformance

import (
	"context"
	"errors"
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestRecordingGraphQueryCapturesErrorText pins that a failed read records
// the run error text: a failures-kind divergence without the error is
// unactionable triage.
func TestRecordingGraphQueryCapturesErrorText(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	query := WrapGraphQuery(stubDifferentialGraphQuery{err: errors.New("boom")}, recorder, "neo4j")
	if _, err := query.Run(context.Background(), "MATCH (n) RETURN n", nil); err == nil {
		t.Fatal("expected the inner error to propagate")
	}
	records := recorder.Records()
	if len(records) != 1 || !records[0].Failed {
		t.Fatalf("records = %+v, want one failed record", records)
	}
	if !strings.Contains(records[0].Error, "boom") {
		t.Fatalf("record error = %q, want the run error text", records[0].Error)
	}
}

// TestRecordingExecutorCapturesErrorText pins the same contract for writes:
// a failed execution records the execution error text.
func TestRecordingExecutorCapturesErrorText(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	inner := &stubDifferentialExecutor{err: errors.New("commit failed")}
	exec := WrapExecutor(inner, recorder, "nornicdb")
	stmt := sourcecypher.Statement{Cypher: "MERGE (a)"}
	if err := exec.Execute(context.Background(), stmt); err == nil {
		t.Fatal("expected the inner error to propagate")
	}
	records := recorder.Records()
	if len(records) != 1 || !records[0].Failed {
		t.Fatalf("records = %+v, want one failed record", records)
	}
	if !strings.Contains(records[0].Error, "commit failed") {
		t.Fatalf("record error = %q, want the execution error text", records[0].Error)
	}
}

// TestCompareRecordingsSurfacesFailureError pins that a failures-kind
// difference carries the recorded error text in its detail, so the oracle
// report names the failure instead of only counting it.
func TestCompareRecordingsSurfacesFailureError(t *testing.T) {
	t.Parallel()
	fp := DifferentialFingerprint{Statement: "MERGE (n:File {path: $path})", Parameters: `{"path":"a"}`}
	a := []DifferentialRecord{{Backend: "nornicdb", Fingerprint: fp, Failed: true, Error: "boom"}}
	b := []DifferentialRecord{{Backend: "neo4j", Fingerprint: fp}}
	diffs := CompareRecordings(a, b)
	if len(diffs) != 1 {
		t.Fatalf("differences = %v, want the one-sided failure", diffs)
	}
	if diffs[0].Kind != "failures" {
		t.Fatalf("kind = %q, want failures", diffs[0].Kind)
	}
	if !strings.Contains(diffs[0].Detail, "boom") {
		t.Fatalf("detail = %q, want the recorded error text", diffs[0].Detail)
	}
}
