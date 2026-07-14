// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
)

// fakeQuarantinedFactWriter is an in-memory reducer.QuarantinedFactWriter used
// to assert what recordQuarantinedFacts/persistQuarantinedFacts hand off to a
// durable writer, without a live Postgres connection.
type fakeQuarantinedFactWriter struct {
	writes    [][]QuarantinedFactRecord
	failNext  bool
	callCount int
}

func (w *fakeQuarantinedFactWriter) WriteQuarantinedFacts(_ context.Context, records []QuarantinedFactRecord) error {
	w.callCount++
	if w.failNext {
		w.failNext = false
		return errors.New("simulated durable write failure")
	}
	// Copy defensively so a caller mutating its slice afterward cannot
	// retroactively change what this fake observed.
	cp := make([]QuarantinedFactRecord, len(records))
	copy(cp, records)
	w.writes = append(w.writes, cp)
	return nil
}

// TestWithQuarantineWriterRoundTrip proves the context helpers in
// quarantine_writer.go round-trip a writer, and that a nil writer never gets
// stashed (quarantineWriterFromContext must return nil either way, so
// recordQuarantinedFacts' nil-writer no-op path is reachable from an
// unconfigured Service).
func TestWithQuarantineWriterRoundTrip(t *testing.T) {
	t.Parallel()

	if got := quarantineWriterFromContext(context.Background()); got != nil {
		t.Fatalf("quarantineWriterFromContext(bare context) = %v, want nil", got)
	}

	writer := &fakeQuarantinedFactWriter{}
	ctx := WithQuarantineWriter(context.Background(), writer)
	got := quarantineWriterFromContext(ctx)
	if got != writer {
		t.Fatalf("quarantineWriterFromContext() = %v, want the stashed writer", got)
	}

	// A nil writer must not overwrite ctx (WithQuarantineWriter is a no-op),
	// so a writer already on ctx from an outer call survives.
	unchanged := WithQuarantineWriter(ctx, nil)
	if quarantineWriterFromContext(unchanged) != writer {
		t.Fatalf("WithQuarantineWriter(ctx, nil) must not clear an existing writer")
	}
}

// TestRecordQuarantinedFactsPersistsThroughContextWriter proves (a) rows are
// written for quarantined facts: recordQuarantinedFacts, given a writer
// stashed on ctx, builds and hands off exactly one QuarantinedFactRecord per
// quarantinedFact, with every field the durable table needs.
func TestRecordQuarantinedFactsPersistsThroughContextWriter(t *testing.T) {
	t.Parallel()

	writer := &fakeQuarantinedFactWriter{}
	ctx := WithQuarantineWriter(context.Background(), writer)

	quarantined := []quarantinedFact{
		{factID: "fact-1", factKind: "aws_resource", field: "account_id", classification: "input_invalid"},
		{factID: "fact-2", factKind: "aws_resource", field: "region", classification: "input_invalid"},
	}

	count := recordQuarantinedFacts(ctx, nil, DomainAWSResourceMaterialization, "scope-1", "gen-1", quarantined)

	if count != 2 {
		t.Fatalf("recordQuarantinedFacts() count = %d, want 2", count)
	}
	if writer.callCount != 1 {
		t.Fatalf("writer.callCount = %d, want 1 (one batched round trip per intent)", writer.callCount)
	}
	if len(writer.writes) != 1 || len(writer.writes[0]) != 2 {
		t.Fatalf("writer observed %v, want one batch of 2 records", writer.writes)
	}
	got := writer.writes[0]
	want := []QuarantinedFactRecord{
		{FactID: "fact-1", FactKind: "aws_resource", MissingField: "account_id", FailureClass: "input_invalid", Domain: string(DomainAWSResourceMaterialization), ScopeID: "scope-1", GenerationID: "gen-1"},
		{FactID: "fact-2", FactKind: "aws_resource", MissingField: "region", FailureClass: "input_invalid", Domain: string(DomainAWSResourceMaterialization), ScopeID: "scope-1", GenerationID: "gen-1"},
	}
	for i := range want {
		if got[i].FactID != want[i].FactID || got[i].FactKind != want[i].FactKind ||
			got[i].MissingField != want[i].MissingField || got[i].FailureClass != want[i].FailureClass ||
			got[i].Domain != want[i].Domain || got[i].ScopeID != want[i].ScopeID ||
			got[i].GenerationID != want[i].GenerationID {
			t.Fatalf("record[%d] = %+v, want %+v", i, got[i], want[i])
		}
		if got[i].DecidedAt.IsZero() {
			t.Fatalf("record[%d].DecidedAt is zero, want a stamped decision time", i)
		}
	}
}

// TestRecordQuarantinedFactsWriteFailureIsNonFatal proves (b) a persist error
// does NOT fail the intent: recordQuarantinedFacts has no error return at
// all, so a durable-write failure can only ever be observed as a swallowed,
// logged, counted side effect — it is architecturally impossible for it to
// propagate to the handler's Result/error path. This test proves the
// swallow actually happens (the call completes, returns the correct count,
// and does not panic) rather than merely asserting the signature.
func TestRecordQuarantinedFactsWriteFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	writer := &fakeQuarantinedFactWriter{failNext: true}
	ctx := WithQuarantineWriter(context.Background(), writer)

	quarantined := []quarantinedFact{
		{factID: "fact-1", factKind: "aws_resource", field: "account_id", classification: "input_invalid"},
	}

	count := recordQuarantinedFacts(ctx, nil, DomainAWSResourceMaterialization, "scope-1", "gen-1", quarantined)

	if count != 1 {
		t.Fatalf("recordQuarantinedFacts() count = %d, want 1 even though the durable write failed", count)
	}
	if writer.callCount != 1 {
		t.Fatalf("writer.callCount = %d, want 1 (persistQuarantinedFacts must still attempt the write)", writer.callCount)
	}
	if len(writer.writes) != 0 {
		t.Fatalf("writer.writes = %v, want none recorded since WriteQuarantinedFacts returned an error", writer.writes)
	}
}

// TestRecordQuarantinedFactsNilWriterIsNoOp proves the default (unconfigured
// Service.QuarantineWriter, and every handler-level unit test that does not
// call WithQuarantineWriter) still returns the correct count and never
// panics on a nil writer.
func TestRecordQuarantinedFactsNilWriterIsNoOp(t *testing.T) {
	t.Parallel()

	quarantined := []quarantinedFact{
		{factID: "fact-1", factKind: "aws_resource", field: "account_id", classification: "input_invalid"},
	}

	count := recordQuarantinedFacts(context.Background(), nil, DomainAWSResourceMaterialization, "scope-1", "gen-1", quarantined)

	if count != 1 {
		t.Fatalf("recordQuarantinedFacts() count = %d, want 1", count)
	}
}

// TestRecordQuarantinedFactsEmptyBatchSkipsWriter proves an intent that
// quarantines nothing never calls the writer at all (persistQuarantinedFacts'
// early return), so a healthy scope generation with zero malformed facts adds
// no durable-write overhead.
func TestRecordQuarantinedFactsEmptyBatchSkipsWriter(t *testing.T) {
	t.Parallel()

	writer := &fakeQuarantinedFactWriter{}
	ctx := WithQuarantineWriter(context.Background(), writer)

	count := recordQuarantinedFacts(ctx, nil, DomainAWSResourceMaterialization, "scope-1", "gen-1", nil)

	if count != 0 {
		t.Fatalf("recordQuarantinedFacts() count = %d, want 0", count)
	}
	if writer.callCount != 0 {
		t.Fatalf("writer.callCount = %d, want 0 for an empty quarantine batch", writer.callCount)
	}
}
