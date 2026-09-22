// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"errors"
	"fmt"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// ValueFlowInputsNotReadyFailureClass is the durable failure_class the #6923
// value-flow refresh singleton self-classifies with when its input fence
// (go/internal/storage/postgres/value_flow_refresh_ack.go) finds an
// active-generation writer of the CAN_PERFORM/USES/RUNS_IN chain still
// nonterminal, or an open runs_in/invokes_cloud_action shared-projection
// intent. A retrying row in this class is deferred until those writers
// commit, not failing on its own merits, so it is enrolled in
// nonCountingReducerRetryFailureClasses
// (go/internal/storage/postgres/reducer_queue_readiness_sql.go) to exempt it
// from the retry budget, exactly like #6887's CloudAdmissionNotReadyFailureClass.
//
// Declared at this depth (internal/reducer/crossscope, not
// code/value/refresh) because TestEveryReadinessFailureClassIsEnrolled only
// walks internal/reducer and its IMMEDIATE subdirectories
// (reducer_queue_readiness_enrollment_test.go); a class declared three levels
// down would be invisible to that guard.
const ValueFlowInputsNotReadyFailureClass = "value_flow_inputs_not_ready"

// valueFlowInputsNotReadyError marks a #6923 value-flow refresh fence refusal
// as a readiness-gate miss: the fence found a nonterminal writer of the
// cloud-sink chain on an active generation, so the singleton is waiting on an
// upstream phase, not failing on its own merits. The durable queue re-runs
// the singleton once those writers drain, or once the bound
// (crossscope.ProducerReadinessMaxWait) elapses and the handler solves anyway.
type valueFlowInputsNotReadyError struct {
	cause error
}

func (e valueFlowInputsNotReadyError) Error() string { return e.cause.Error() }

func (e valueFlowInputsNotReadyError) Unwrap() error { return e.cause }

func (valueFlowInputsNotReadyError) Retryable() bool { return true }

func (valueFlowInputsNotReadyError) FailureClass() string {
	return ValueFlowInputsNotReadyFailureClass
}

// ClassifyValueFlowInputsError maps a value-flow refresh fence failure to the
// queue's readiness vocabulary: an error wrapping
// reducercontract.ErrValueFlowInputsUndrained becomes a Retryable,
// non-counting valueFlowInputsNotReadyError that still unwraps to the
// sentinel; any other error (including nil) is returned unchanged.
func ClassifyValueFlowInputsError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, reducercontract.ErrValueFlowInputsUndrained) {
		return valueFlowInputsNotReadyError{cause: err}
	}
	return err
}

// WrapValueFlowInputsUndrained builds the classified fence-refusal error from
// the pending rows the fence found: the sentinel wrapped with the pending
// detail, then classified through ClassifyValueFlowInputsError. Exported so
// go/internal/reducer/code/value/refresh and its tests share one
// construction path with the error text and classification kept together.
func WrapValueFlowInputsUndrained(pending []string) error {
	return ClassifyValueFlowInputsError(fmt.Errorf("%w: %s", reducercontract.ErrValueFlowInputsUndrained, strings.Join(pending, ", ")))
}
