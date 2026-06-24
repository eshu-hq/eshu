// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// ScannerWorkerAnalysisFactKind identifies one source fact emitted by an
	// isolated security analyzer. Reducers must convert these source facts into
	// any user-facing findings.
	ScannerWorkerAnalysisFactKind = "scanner_worker.analysis"
	// ScannerWorkerWarningFactKind identifies a non-fatal scanner-worker
	// warning such as an analyzer timeout, skipped target, or partial result.
	ScannerWorkerWarningFactKind = "scanner_worker.warning"

	// ScannerWorkerSchemaVersionV1 is the first scanner-worker source fact
	// schema.
	ScannerWorkerSchemaVersionV1 = "1.0.0"
)

var scannerWorkerFactKinds = []string{
	ScannerWorkerAnalysisFactKind,
	ScannerWorkerWarningFactKind,
}

var scannerWorkerSchemaVersions = map[string]string{
	ScannerWorkerAnalysisFactKind: ScannerWorkerSchemaVersionV1,
	ScannerWorkerWarningFactKind:  ScannerWorkerSchemaVersionV1,
}

// ScannerWorkerFactKinds returns the accepted scanner-worker source fact kinds
// in contract order.
func ScannerWorkerFactKinds() []string {
	return slices.Clone(scannerWorkerFactKinds)
}

// ScannerWorkerSchemaVersion returns the schema version for a scanner-worker
// source fact kind.
func ScannerWorkerSchemaVersion(factKind string) (string, bool) {
	version, ok := scannerWorkerSchemaVersions[factKind]
	return version, ok
}
