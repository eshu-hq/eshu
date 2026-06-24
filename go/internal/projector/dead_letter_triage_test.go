// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// retryableErr is a test error that opts into bounded retry via the canonical
// Retryable() convention used on the live reducer/projector queue path.
type retryableErr struct{ msg string }

func (e retryableErr) Error() string { return e.msg }
func (retryableErr) Retryable() bool { return true }

// TestTriageFailureRetryExhausted proves a transient (retryable) cause that has
// exhausted its retry budget is dead-lettered with the retry_exhausted triage
// class and a retryable disposition, so an operator can tell it died from
// transient pressure (safe to replay) rather than a terminal defect.
func TestTriageFailureRetryExhausted(t *testing.T) {
	t.Parallel()

	cause := retryableErr{msg: "neo4j deadlock after retries"}
	record := TriageFailure(cause, "project_facts", true, true)

	if record.FailureClass != string(TriageClassRetryExhausted) {
		t.Fatalf("FailureClass = %q, want %q", record.FailureClass, TriageClassRetryExhausted)
	}
	if !triageRecordHas(record.Details, "disposition=retryable") {
		t.Fatalf("details = %q, want disposition=retryable", record.Details)
	}
	if !triageRecordHas(record.Details, "stage=project_facts") {
		t.Fatalf("details = %q, want stage=project_facts", record.Details)
	}
}

// TestTriageFailureTerminalInputInvalid proves a non-retryable input-validation
// cause is dead-lettered as a terminal, non-retryable class. Replaying it
// unchanged would just fail again, so triage must mark it non-retryable.
func TestTriageFailureTerminalInputInvalid(t *testing.T) {
	t.Parallel()

	cause := NewInputValidationError("bad scope_id")
	record := TriageFailure(cause, "project_work_item", false, false)

	if record.FailureClass != string(TriageClassInputInvalid) {
		t.Fatalf("FailureClass = %q, want %q", record.FailureClass, TriageClassInputInvalid)
	}
	if !triageRecordHas(record.Details, "disposition=non_retryable") {
		t.Fatalf("details = %q, want disposition=non_retryable", record.Details)
	}
}

// TestTriageFailurePoisonProjectionBug proves an unclassified terminal failure
// is dead-lettered as a projection_bug needing manual review (the poison
// bucket), not silently retried.
func TestTriageFailurePoisonProjectionBug(t *testing.T) {
	t.Parallel()

	cause := errors.New("nil map dereference in projector")
	record := TriageFailure(cause, "project_relationships", false, false)

	if record.FailureClass != string(TriageClassProjectionBug) {
		t.Fatalf("FailureClass = %q, want %q", record.FailureClass, TriageClassProjectionBug)
	}
	if !triageRecordHas(record.Details, "disposition=manual_review") {
		t.Fatalf("details = %q, want disposition=manual_review", record.Details)
	}
}

// TestTriageFailureConsistencyWithRetryable proves the triage disposition never
// contradicts the canonical Retryable() retry authority: a cause the live path
// treated as retryable must never be triaged as a terminal non_retryable class,
// and a non-retryable cause must never be triaged as retryable. This is the
// reconciliation guarantee for issue #3514 — ClassifyFailure enriches the
// durable record but Retryable() stays the decision authority.
func TestTriageFailureConsistencyWithRetryable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		cause     error
		retryable bool
		exhausted bool
		wantClass TriageClass
		wantDisp  string
	}{
		{
			name:      "retryable_not_exhausted_requeued_view",
			cause:     retryableErr{msg: "transient"},
			retryable: true,
			exhausted: false,
			wantClass: TriageClassRetrying,
			wantDisp:  "disposition=retryable",
		},
		{
			name:      "retryable_exhausted",
			cause:     retryableErr{msg: "transient"},
			retryable: true,
			exhausted: true,
			wantClass: TriageClassRetryExhausted,
			wantDisp:  "disposition=retryable",
		},
		{
			name:      "neo4j_transient_but_marked_terminal_by_authority",
			cause:     &neo4jdriver.Neo4jError{Code: "Neo.TransientError.Transaction.DeadlockDetected", Msg: "deadlock"},
			retryable: false,
			exhausted: false,
			// Authority says terminal: triage must respect it and not invent a
			// retryable disposition from the underlying transient shape.
			wantClass: TriageClassDependencyUnavailable,
			wantDisp:  "disposition=non_retryable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			record := TriageFailure(tc.cause, "project_facts", tc.retryable, tc.exhausted)
			if record.FailureClass != string(tc.wantClass) {
				t.Fatalf("FailureClass = %q, want %q", record.FailureClass, tc.wantClass)
			}
			if !triageRecordHas(record.Details, tc.wantDisp) {
				t.Fatalf("details = %q, want %q", record.Details, tc.wantDisp)
			}
		})
	}
}

// TestTriageFailureContextCanceledTimeout proves a context-cancellation cause
// that the authority marked retryable but exhausted lands in the retry_exhausted
// bucket while preserving the timeout failure code for diagnosis.
func TestTriageFailureContextCanceledTimeout(t *testing.T) {
	t.Parallel()

	record := TriageFailure(context.DeadlineExceeded, "load_facts", true, true)
	if record.FailureClass != string(TriageClassRetryExhausted) {
		t.Fatalf("FailureClass = %q, want %q", record.FailureClass, TriageClassRetryExhausted)
	}
	if !triageRecordHas(record.Details, "code=") {
		t.Fatalf("details = %q, want a failure code", record.Details)
	}
}

// TestTriageDispositionIsConsistent proves the standalone consistency predicate
// rejects any (retryable, disposition) pair that would mislead an operator.
func TestTriageDispositionIsConsistent(t *testing.T) {
	t.Parallel()

	if TriageDispositionConflicts(true, RetryDispositionNonRetryable) == false {
		t.Fatal("retryable cause with non_retryable disposition must be flagged as a conflict")
	}
	if TriageDispositionConflicts(false, RetryDispositionRetryable) == false {
		t.Fatal("non-retryable cause with retryable disposition must be flagged as a conflict")
	}
	if TriageDispositionConflicts(true, RetryDispositionRetryable) {
		t.Fatal("retryable cause with retryable disposition must not conflict")
	}
	if TriageDispositionConflicts(false, RetryDispositionManualReview) {
		t.Fatal("non-retryable cause with manual_review disposition must not conflict")
	}
}

func triageRecordHas(details, want string) bool {
	return strings.Contains(details, want)
}
