// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	codedataflowv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codedataflow/v1"
)

// CodeDataflowScannedSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_dataflow_scanned" payload.
const CodeDataflowScannedSchemaID = schemaBaseID + "codedataflow/v1/code_dataflow_scanned.schema.json"

// CodeDataflowScannedSchema returns the JSON Schema bytes for
// codedataflowv1.DataflowScanned.
func CodeDataflowScannedSchema() ([]byte, error) {
	return reflectSchema(CodeDataflowScannedSchemaID, "Eshu code_dataflow_scanned Payload (schema version 1)", &codedataflowv1.DataflowScanned{})
}

// CodeDataflowFunctionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_dataflow_function" payload.
const CodeDataflowFunctionSchemaID = schemaBaseID + "codedataflow/v1/code_dataflow_function.schema.json"

// CodeDataflowFunctionSchema returns the JSON Schema bytes for
// codedataflowv1.DataflowFunction.
func CodeDataflowFunctionSchema() ([]byte, error) {
	return reflectSchema(CodeDataflowFunctionSchemaID, "Eshu code_dataflow_function Payload (schema version 1)", &codedataflowv1.DataflowFunction{})
}

// CodeFunctionSummarySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_function_summary" payload.
const CodeFunctionSummarySchemaID = schemaBaseID + "codedataflow/v1/code_function_summary.schema.json"

// CodeFunctionSummarySchema returns the JSON Schema bytes for
// codedataflowv1.FunctionSummary.
func CodeFunctionSummarySchema() ([]byte, error) {
	return reflectSchema(CodeFunctionSummarySchemaID, "Eshu code_function_summary Payload (schema version 1)", &codedataflowv1.FunctionSummary{})
}

// CodeFunctionSourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_function_source" payload.
const CodeFunctionSourceSchemaID = schemaBaseID + "codedataflow/v1/code_function_source.schema.json"

// CodeFunctionSourceSchema returns the JSON Schema bytes for
// codedataflowv1.FunctionSource.
func CodeFunctionSourceSchema() ([]byte, error) {
	return reflectSchema(CodeFunctionSourceSchemaID, "Eshu code_function_source Payload (schema version 1)", &codedataflowv1.FunctionSource{})
}

// CodeTaintEvidenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_taint_evidence" payload.
const CodeTaintEvidenceSchemaID = schemaBaseID + "codedataflow/v1/code_taint_evidence.schema.json"

// CodeTaintEvidenceSchema returns the JSON Schema bytes for
// codedataflowv1.TaintEvidence.
func CodeTaintEvidenceSchema() ([]byte, error) {
	return reflectSchema(CodeTaintEvidenceSchemaID, "Eshu code_taint_evidence Payload (schema version 1)", &codedataflowv1.TaintEvidence{})
}

// CodeInterprocEvidenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_interproc_evidence" payload.
const CodeInterprocEvidenceSchemaID = schemaBaseID + "codedataflow/v1/code_interproc_evidence.schema.json"

// CodeInterprocEvidenceSchema returns the JSON Schema bytes for
// codedataflowv1.InterprocEvidence.
func CodeInterprocEvidenceSchema() ([]byte, error) {
	return reflectSchema(CodeInterprocEvidenceSchemaID, "Eshu code_interproc_evidence Payload (schema version 1)", &codedataflowv1.InterprocEvidence{})
}
