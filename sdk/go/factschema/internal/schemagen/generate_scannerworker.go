// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	scannerworkerv1 "github.com/eshu-hq/eshu/sdk/go/factschema/scannerworker/v1"
)

// ScannerWorkerAnalysisSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "scanner_worker.analysis" payload.
const ScannerWorkerAnalysisSchemaID = schemaBaseID + "scannerworker/v1/analysis.schema.json"

// ScannerWorkerAnalysisSchema returns the JSON Schema bytes for
// scannerworkerv1.Analysis.
func ScannerWorkerAnalysisSchema() ([]byte, error) {
	return reflectSchema(ScannerWorkerAnalysisSchemaID, "Eshu scanner_worker.analysis Payload (schema version 1)", &scannerworkerv1.Analysis{})
}

// ScannerWorkerWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "scanner_worker.warning" payload.
const ScannerWorkerWarningSchemaID = schemaBaseID + "scannerworker/v1/warning.schema.json"

// ScannerWorkerWarningSchema returns the JSON Schema bytes for
// scannerworkerv1.Warning.
func ScannerWorkerWarningSchema() ([]byte, error) {
	return reflectSchema(ScannerWorkerWarningSchemaID, "Eshu scanner_worker.warning Payload (schema version 1)", &scannerworkerv1.Warning{})
}
