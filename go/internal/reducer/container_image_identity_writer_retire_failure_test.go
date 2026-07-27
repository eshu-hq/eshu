// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// failingContainerImageIdentityExecer records every statement and fails the one
// whose text contains failOn, so a test can inject a failure at an exact point
// in the writer's statement sequence.
//
// fakeWorkloadIdentityExecer always returns a nil error, which is why the claim
// "a failed insert leaves the previous decisions in place" could be asserted in
// prose but never tested: with no way to make the insert fail, no test could
// observe whether the retire still ran after it.
type failingContainerImageIdentityExecer struct {
	execs  []fakeWorkloadIdentityExecCall
	failOn string
	err    error
	// retiredRows is what a successful retire reports as RowsAffected, so a
	// test can distinguish "retired nothing" from "retired a non-empty set".
	retiredRows int64
}

func (f *failingContainerImageIdentityExecer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakeWorkloadIdentityExecCall{query: query, args: args})
	if f.failOn != "" && strings.Contains(query, f.failOn) {
		return nil, f.err
	}
	return containerImageIdentityExecResult{rows: f.retiredRows}, nil
}

// containerImageIdentityExecResult reports a caller-chosen RowsAffected so the
// writer's retired-row accounting can be asserted against a known number.
type containerImageIdentityExecResult struct{ rows int64 }

func (containerImageIdentityExecResult) LastInsertId() (int64, error) { return 0, nil }
func (r containerImageIdentityExecResult) RowsAffected() (int64, error) {
	return r.rows, nil
}

// containerImageIdentityFailureWrite is a minimal fenced write with one
// canonical decision.
func containerImageIdentityFailureWrite() ContainerImageIdentityWrite {
	return ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		EvidenceAsOf: time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
		Decisions: []ContainerImageIdentityDecision{
			containerImageIdentityDecisionForOutcome(
				"registry.example.com/team/api:prod",
				testContainerDigest,
				ContainerImageIdentityExactDigest,
			),
		},
	}
}

// TestContainerImageIdentityWriterDoesNotRetireWhenInsertFails is the missing
// proof behind the insert-then-retire ordering claim.
//
// The ordering exists so a failed insert leaves the previous generation's
// decisions in place instead of clearing them and writing nothing. That was
// asserted in comments and never tested, because the only execer available
// returned nil for every statement. With the insert failing, ZERO retire
// statements must reach the database — a retire-anyway regression would delete
// the prior decisions and replace them with nothing at all.
func TestContainerImageIdentityWriterDoesNotRetireWhenInsertFails(t *testing.T) {
	t.Parallel()

	insertFailure := errors.New("connection reset by peer")
	db := &failingContainerImageIdentityExecer{
		failOn: "INSERT INTO fact_records",
		err:    insertFailure,
	}
	writer := PostgresContainerImageIdentityWriter{DB: db}

	_, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityFailureWrite(),
	)
	if err == nil {
		t.Fatal("WriteContainerImageIdentityDecisions() error = nil, want the insert failure surfaced")
	}
	if !errors.Is(err, insertFailure) {
		t.Fatalf("error = %v, want it to wrap the insert failure", err)
	}

	for _, call := range db.execs {
		if isContainerImageIdentityRetireStatement(call.query) {
			t.Fatal("a retire statement was issued after the insert failed; " +
				"the prior generation's decisions would be deleted and nothing written in their place")
		}
	}
}

// TestContainerImageIdentityWriterReportsRetiredRowCount is the observability
// half of the retire.
//
// The retire deletes durable decisions. "It runs inside the instrumented
// ExecContext wrapper" records that A write happened; it never records WHAT was
// destroyed. The writer therefore surfaces the affected row count the same way
// PostgresEshuSearchDocumentWriter.retireSearchDocumentFacts does, so an
// operator reading the reducer result can see how many decisions a pass retired.
func TestContainerImageIdentityWriterReportsRetiredRowCount(t *testing.T) {
	t.Parallel()

	db := &failingContainerImageIdentityExecer{retiredRows: 3}
	writer := PostgresContainerImageIdentityWriter{DB: db}

	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityFailureWrite(),
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if got, want := result.Retired, 3; got != want {
		t.Fatalf("Retired = %d, want %d (the retire's RowsAffected)", got, want)
	}
	if !strings.Contains(result.EvidenceSummary, "retired=3") {
		t.Fatalf("EvidenceSummary = %q, want it to report retired=3", result.EvidenceSummary)
	}
}

// TestContainerImageIdentityWriterReportsZeroRetiredWhenNothingWasSuperseded
// pins the ordinary case, so the retired count cannot silently become a
// constant.
func TestContainerImageIdentityWriterReportsZeroRetiredWhenNothingWasSuperseded(t *testing.T) {
	t.Parallel()

	db := &failingContainerImageIdentityExecer{retiredRows: 0}
	writer := PostgresContainerImageIdentityWriter{DB: db}

	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityFailureWrite(),
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if got, want := result.Retired, 0; got != want {
		t.Fatalf("Retired = %d, want %d", got, want)
	}
	if !strings.Contains(result.EvidenceSummary, "retired=0") {
		t.Fatalf("EvidenceSummary = %q, want it to report retired=0", result.EvidenceSummary)
	}
}

// TestContainerImageIdentityWriterFlagsBlindRetireOfPriorDecisions covers the
// hazard the fence cannot: an evidence-visibility gap that looks exactly like a
// demotion.
//
// classifyContainerImageRef answers `unresolved` when index.observationsForDigest
// returns ZERO observations, which is the same answer it gives for a reference
// that genuinely has no registry evidence. A pass that runs while the cross-scope
// OCI facts are not visible therefore lands EVERY decision non-canonical, hands
// the writer an empty keep-set, and retires the whole (scope, generation)
// partition — a pass that before the retire existed was a harmless no-op. The
// fencing token does not help, because this pass read its (empty) evidence LAST
// and so ranks highest.
//
// The write is still performed: the empty keep-set is genuinely correct for a
// real demotion, and the writer cannot tell the two apart from where it sits. It
// must be LOUD about it instead, so an operator has something to find at 3 AM.
func TestContainerImageIdentityWriterFlagsBlindRetireOfPriorDecisions(t *testing.T) {
	t.Parallel()

	db := &failingContainerImageIdentityExecer{retiredRows: 7}
	writer := PostgresContainerImageIdentityWriter{DB: db}

	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		ContainerImageIdentityWrite{
			IntentID:     "intent-image-identity",
			ScopeID:      "repo:team-api",
			GenerationID: "generation-git",
			SourceSystem: "git",
			EvidenceAsOf: time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
			Decisions: []ContainerImageIdentityDecision{
				{
					ImageRef:        "registry.example.com/team/api:prod",
					Outcome:         ContainerImageIdentityUnresolved,
					Reason:          "no registry digest observation matched the image reference",
					CanonicalWrites: 0,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := result.Retired, 7; got != want {
		t.Fatalf("Retired = %d, want %d", got, want)
	}
	if !result.RetiredWithoutCanonicalWrites {
		t.Fatal("RetiredWithoutCanonicalWrites = false; a write that retired a non-empty prior set " +
			"while producing zero canonical decisions must be flagged, not silently accepted")
	}
	if !strings.Contains(result.EvidenceSummary, "retired_without_canonical_writes=true") {
		t.Fatalf(
			"EvidenceSummary = %q, want it to name the blind retire",
			result.EvidenceSummary,
		)
	}
}

// TestContainerImageIdentityWriterDoesNotFlagAnEmptyRetire keeps the blind-retire
// flag from firing on the common, harmless case: a generation that had nothing
// durable to begin with.
func TestContainerImageIdentityWriterDoesNotFlagAnEmptyRetire(t *testing.T) {
	t.Parallel()

	db := &failingContainerImageIdentityExecer{retiredRows: 0}
	writer := PostgresContainerImageIdentityWriter{DB: db}

	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		ContainerImageIdentityWrite{
			IntentID:     "intent-image-identity",
			ScopeID:      "repo:team-api",
			GenerationID: "generation-git",
			SourceSystem: "git",
			EvidenceAsOf: time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if result.RetiredWithoutCanonicalWrites {
		t.Fatal("RetiredWithoutCanonicalWrites = true for a retire that deleted nothing")
	}
}
