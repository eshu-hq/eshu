// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"bytes"
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
)

// TestRunAllRelationshipEvidenceMemoGateFailureDegradesWithoutFailingPass pins
// the memo gate's one non-fatal error (arbiter a9-408): a failed memo lookup
// falls back to a full load and the pass succeeds. The memo_gate block must
// then log exactly one deferred_backfill_block_degraded line (and neither a
// failed nor a completed line: failed means the pass failed), keep Error status
// with fallback=true on its span, and still record its histogram sample. The
// existing gate_failed line stays the failure signal.
func TestRunAllRelationshipEvidenceMemoGateFailureDegradesWithoutFailingPass(t *testing.T) {
	store, database := newBlockPassStore(t)
	database.memoErr = errors.New("boom memo lookup")

	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })
	var buf bytes.Buffer
	log.SetOutput(&buf)

	recorder, tracer := recordingTracer()
	reader, instruments := fanInMetricHarness(t)
	if _, err := store.RunAllRelationshipEvidence(context.Background(), tracer, instruments); err != nil {
		t.Fatalf("RunAllRelationshipEvidence() error = %v, want nil (gate error is non-fatal)", err)
	}

	ended := recorder.Ended()
	gate := findEndedSpan(t, ended, blockSpanPrefix+"memo_gate")
	if gate.Status().Code != codes.Error {
		t.Errorf("memo_gate span status = %v, want Error", gate.Status().Code)
	}
	var fallback, found bool
	for _, attr := range gate.Attributes() {
		if string(attr.Key) == "fallback" {
			fallback, found = attr.Value.AsBool(), true
		}
	}
	if !found || !fallback {
		t.Errorf("memo_gate fallback attribute = %v (set=%v), want true", fallback, found)
	}
	if got := blockSpanInt(t, gate, "loaded"); got != 2 {
		t.Errorf("memo_gate loaded = %d, want 2 (full load)", got)
	}
	// The full load ran: fact_load reports both partitions and the one fact.
	fact := findEndedSpan(t, ended, blockSpanPrefix+"fact_load")
	if got := blockSpanInt(t, fact, "partitions"); got != 2 {
		t.Errorf("fact_load partitions = %d, want 2", got)
	}
	if got := blockSpanInt(t, fact, "facts"); got != 1 {
		t.Errorf("fact_load facts = %d, want 1", got)
	}

	output := buf.String()
	degraded := regexp.MustCompile(
		`deferred_backfill_block_degraded block=memo_gate duration_s=\d+\.\d{2} candidate_partitions=2 skipped=0 loaded=2 fallback=true error="[^"]*boom memo lookup"\n`,
	).FindAllString(output, -1)
	if len(degraded) != 1 {
		t.Fatalf("deferred_backfill_block_degraded lines = %d, want exactly 1\noutput:\n%s", len(degraded), output)
	}
	if strings.Contains(output, "deferred_backfill_block_failed") {
		t.Errorf("degraded gate emitted a block_failed line\noutput:\n%s", output)
	}
	if strings.Contains(output, blockLogMarker+"block=memo_gate") {
		t.Errorf("degraded gate also emitted a completed line\noutput:\n%s", output)
	}
	if !strings.Contains(output, "deferred_backfill_partition_memo_gate_failed") {
		t.Errorf("existing gate_failed line missing\noutput:\n%s", output)
	}
	if got := collectBlockDurationPoints(t, reader)["memo_gate"]; got != 1 {
		t.Errorf("memo_gate histogram points = %d, want 1 (the gate ran and fell back)", got)
	}
}
