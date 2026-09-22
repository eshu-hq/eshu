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
// row-set wobble #6923 reports. The refresh handler
// (internal/reducer/code/value/refresh) raises it through
// crossscope.WrapValueFlowInputsUndrained, which wraps the sentinel with the
// pending rows the storage fence returned and classifies the result as the
// non-counting readiness miss crossscope.ValueFlowInputsNotReadyFailureClass,
// so the durable queue retries the singleton without eroding its attempt
// budget. It lives here beside the other readiness sentinels
// (ErrCloudAdmissionUndrained) because contract is the one package every
// reducer family and the storage layer may import; storage/postgres does not
// reference it today.
var ErrValueFlowInputsUndrained = errors.New("value-flow refresh inputs not drained for an active generation")
