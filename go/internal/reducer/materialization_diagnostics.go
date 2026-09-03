// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

// materialization_diagnostics.go is the reducer-root spelling of the shared
// materialization diagnostic signals. The contract, the three operator states
// it encodes, and the reason the values are SubSignals rather than SubDurations
// all live with the implementation in
// [reducercontract.MaterializationDiagnosticSignals]; the helper moved to the
// shared tier with the platform family (#6061) so the families in subpackages
// and the domains still in the root emit an identical key set.

const (
	// diagnosticSignalInputReady aliases
	// [reducercontract.DiagnosticSignalInputReady].
	diagnosticSignalInputReady = reducercontract.DiagnosticSignalInputReady
	// diagnosticSignalWrittenRows aliases
	// [reducercontract.DiagnosticSignalWrittenRows].
	diagnosticSignalWrittenRows = reducercontract.DiagnosticSignalWrittenRows
)

// materializationDiagnosticSignals forwards to
// [reducercontract.MaterializationDiagnosticSignals].
func materializationDiagnosticSignals(inputReady bool, writtenRows int) map[string]float64 {
	return reducercontract.MaterializationDiagnosticSignals(inputReady, writtenRows)
}
