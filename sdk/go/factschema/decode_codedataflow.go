// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	codedataflowv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codedataflow/v1"
)

// DecodeCodeDataflowScanned decodes env.Payload into the latest
// codedataflowv1.DataflowScanned struct for the "code_dataflow_scanned" fact
// kind, dispatching on env.SchemaVersion major per Contract System v1 §3.2.
func DecodeCodeDataflowScanned(env Envelope) (codedataflowv1.DataflowScanned, error) {
	return decodeLatestMajor[codedataflowv1.DataflowScanned](FactKindCodeDataflowScanned, env)
}

// EncodeCodeDataflowScanned marshals a codedataflowv1.DataflowScanned into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeCodeDataflowScanned for schema-version-1 payloads.
func EncodeCodeDataflowScanned(scanned codedataflowv1.DataflowScanned) (map[string]any, error) {
	return encodeToPayload(scanned)
}

// DecodeCodeDataflowFunction decodes env.Payload into the latest
// codedataflowv1.DataflowFunction struct for the "code_dataflow_function" fact
// kind. See DecodeCodeDataflowScanned for the dispatch and error contract.
func DecodeCodeDataflowFunction(env Envelope) (codedataflowv1.DataflowFunction, error) {
	return decodeLatestMajor[codedataflowv1.DataflowFunction](FactKindCodeDataflowFunction, env)
}

// EncodeCodeDataflowFunction marshals a codedataflowv1.DataflowFunction into
// the map[string]any payload shape an Envelope carries.
func EncodeCodeDataflowFunction(function codedataflowv1.DataflowFunction) (map[string]any, error) {
	return encodeToPayload(function)
}

// DecodeCodeFunctionSummary decodes env.Payload into the latest
// codedataflowv1.FunctionSummary struct for the "code_function_summary" fact
// kind.
func DecodeCodeFunctionSummary(env Envelope) (codedataflowv1.FunctionSummary, error) {
	return decodeLatestMajor[codedataflowv1.FunctionSummary](FactKindCodeFunctionSummary, env)
}

// EncodeCodeFunctionSummary marshals a codedataflowv1.FunctionSummary into the
// map[string]any payload shape an Envelope carries.
func EncodeCodeFunctionSummary(summary codedataflowv1.FunctionSummary) (map[string]any, error) {
	return encodeToPayload(summary)
}

// DecodeCodeFunctionSource decodes env.Payload into the latest
// codedataflowv1.FunctionSource struct for the "code_function_source" fact
// kind.
func DecodeCodeFunctionSource(env Envelope) (codedataflowv1.FunctionSource, error) {
	return decodeLatestMajor[codedataflowv1.FunctionSource](FactKindCodeFunctionSource, env)
}

// EncodeCodeFunctionSource marshals a codedataflowv1.FunctionSource into the
// map[string]any payload shape an Envelope carries.
func EncodeCodeFunctionSource(source codedataflowv1.FunctionSource) (map[string]any, error) {
	return encodeToPayload(source)
}

// DecodeCodeTaintEvidence decodes env.Payload into the latest
// codedataflowv1.TaintEvidence struct for the "code_taint_evidence" fact kind.
func DecodeCodeTaintEvidence(env Envelope) (codedataflowv1.TaintEvidence, error) {
	return decodeLatestMajor[codedataflowv1.TaintEvidence](FactKindCodeTaintEvidence, env)
}

// EncodeCodeTaintEvidence marshals a codedataflowv1.TaintEvidence into the
// map[string]any payload shape an Envelope carries.
func EncodeCodeTaintEvidence(evidence codedataflowv1.TaintEvidence) (map[string]any, error) {
	return encodeToPayload(evidence)
}

// DecodeCodeInterprocEvidence decodes env.Payload into the latest
// codedataflowv1.InterprocEvidence struct for the "code_interproc_evidence"
// fact kind.
func DecodeCodeInterprocEvidence(env Envelope) (codedataflowv1.InterprocEvidence, error) {
	return decodeLatestMajor[codedataflowv1.InterprocEvidence](FactKindCodeInterprocEvidence, env)
}

// EncodeCodeInterprocEvidence marshals a codedataflowv1.InterprocEvidence into
// the map[string]any payload shape an Envelope carries.
func EncodeCodeInterprocEvidence(evidence codedataflowv1.InterprocEvidence) (map[string]any, error) {
	return encodeToPayload(evidence)
}
