// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "errors"

// ErrValueFlowInputsUndrained is the sentinel the #6923 value-flow refresh
// fence wraps when the graph chain the global fixpoint reads
// (Function->INVOKES_CLOUD_ACTION, RUNS_IN, INSTANCE_OF, USES, CAN_PERFORM,
// and the CloudResource nodes those edges anchor to) has not finished
// materializing: some active generation still has a nonterminal writer of
// that chain, or an open runs_in/invokes_cloud_action shared-projection
// intent. Solving before every writer lands reads a partial graph and
// produces a pre-convergence answer that a later re-trigger corrects — the
// row-set wobble #6923 reports. The storage layer wraps this sentinel with
// the pending rows it found; the refresh handler classifies it as a
// non-counting readiness miss (crossscope.ValueFlowInputsNotReadyFailureClass)
// so the durable queue retries the singleton without eroding its attempt
// budget. It lives in this package because storage/postgres raises it and
// internal/reducer/code/value/refresh inspects it, and neither may import the
// other.
var ErrValueFlowInputsUndrained = errors.New("value-flow refresh inputs not drained for an active generation")
