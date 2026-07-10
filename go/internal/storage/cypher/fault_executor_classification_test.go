// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package cypher

import (
	"errors"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// classifiedIface mirrors the postgres classifiedFailure / reducer
// reducerClassifiedFailure probe: the reducer's failure-class derivation and
// the dead-letter triage both read FailureClass() via errors.As, so asserting
// the error's own method is a direct assertion of the contract those layers
// consume. (Unlike the retry decision, there is no single exported reducer
// function to call for the class, so the interface is asserted directly here.)
type classifiedIface interface{ FailureClass() string }

// TestFaultOnceErrorsModelARealTransientGraphWrite pins the fidelity contract
// for the fail-graph-write-once-then-succeed fault. Both lanes must surface an
// error shaped like the one a REAL exhausted-transient graph write surfaces to
// reducer.Service's WorkSink.Fail -- a retryable, self-classifying
// graph_write_timeout failure (see *neo4jRetryableError). Without that
// contract the reducer's WorkSink.Fail treats the fault error as a plain
// unclassified failure: reducer.IsRetryable is false, so the intent
// dead-letters at attempt 1 and the dead-letter triage default labels a
// reducer-stage graph write as projection_bug -- the fault named
// "...once-then-succeed" could then never succeed. Both lanes collapse to the
// queue-retry path in the in-binary wiring (the FaultingExecutor sits above the
// per-call RetryingExecutor, the documented T1 limitation), so both must carry
// the retryable contract to recover.
func TestFaultOnceErrorsModelARealTransientGraphWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"queue-retry lane", &ifaFaultQueueRetryError{ordinal: 1}},
		{"executor-retry lane", &ifaFaultExecutorRetryShapedError{ordinal: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !reducer.IsRetryable(tc.err) {
				t.Fatalf("fault error must be reducer-retryable so WorkSink.Fail re-enqueues it (queue-retry) instead of dead-lettering at attempt 1")
			}
			var c classifiedIface
			if !errors.As(tc.err, &c) {
				t.Fatalf("fault error must self-classify (FailureClass) so the retrying row is labeled honestly, not defaulted to projection_bug on a dead letter")
			}
			if got := c.FailureClass(); got != GraphWriteTimeoutFailureClass {
				t.Fatalf("fault error FailureClass() = %q, want %q (the class a real exhausted-transient graph write carries)", got, GraphWriteTimeoutFailureClass)
			}
		})
	}
}

// TestFaultOnceErrorsMatchRealTransientContract asserts the injected fault
// errors carry the SAME (Retryable, FailureClass) contract that
// WrapRetryableNeo4jError produces for a real driver-exhausted transient graph
// write, so the fault is a faithful stand-in rather than an invented shape.
func TestFaultOnceErrorsMatchRealTransientContract(t *testing.T) {
	real := WrapRetryableNeo4jError(&neo4jdriver.TransactionExecutionLimit{
		Cause:  "timeout (exceeded max retry time: 30s)",
		Errors: []error{newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock cycle")},
	})
	if !reducer.IsRetryable(real) {
		t.Fatalf("sanity: a real exhausted-transient graph write must be reducer-retryable")
	}
	var rc classifiedIface
	if !errors.As(real, &rc) || rc.FailureClass() != GraphWriteTimeoutFailureClass {
		t.Fatalf("sanity: a real transient graph write must self-classify as %q", GraphWriteTimeoutFailureClass)
	}

	// The injected fault errors must match that contract exactly.
	for _, faultErr := range []error{&ifaFaultQueueRetryError{ordinal: 1}, &ifaFaultExecutorRetryShapedError{ordinal: 1}} {
		if reducer.IsRetryable(faultErr) != reducer.IsRetryable(real) {
			t.Fatalf("fault error Retryable() must match the real transient contract")
		}
		var fc classifiedIface
		if !errors.As(faultErr, &fc) || fc.FailureClass() != rc.FailureClass() {
			t.Fatalf("fault error FailureClass() must match the real transient contract (%q)", rc.FailureClass())
		}
	}
}

// TestPlainUnclassifiedReducerFailureStillTriagesProjectionBug documents the
// upstream default this fix routes AROUND rather than removes: a reducer-stage
// cause that is genuinely plain (no Retryable, no FailureClass) still
// dead-letters and triages to projection_bug via projector.ClassifyFailure.
// The fix makes the fault error NOT plain; it does not change this default,
// which correctly fails closed for a truly-unknown error.
func TestPlainUnclassifiedReducerFailureStillTriagesProjectionBug(t *testing.T) {
	plain := errors.New("write canonical cloud resource nodes: some unknown non-transient failure")
	if got := projector.ClassifyFailure(plain, "reducer").FailureClass; got != projector.FailureClassProjectionBug {
		t.Fatalf("a genuinely unclassified reducer-stage cause should still default to %q, got %q",
			projector.FailureClassProjectionBug, got)
	}
}
