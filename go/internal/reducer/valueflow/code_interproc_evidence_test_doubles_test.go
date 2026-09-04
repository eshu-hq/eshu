// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package valueflow

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer/codetaint"
)

// recordingCodeInterprocEvidenceWriter satisfies
// codetaint.CodeInterprocEvidenceWriter.
//
// This is a hand-kept-in-sync copy of the reducer root's own
// recordingCodeInterprocEvidenceWriter (codedataflow_evidence_test_helpers_test.go)
// and of codetaint's own equivalent test double: Go test files cannot share
// unexported symbols across packages, and this package's
// value_flow_fixpoint_evidence_loader_test.go needs the same shape to drive
// ValueFlowFixpointEvidenceProjector.Writer. If you change
// codetaint.CodeInterprocEvidenceWriter's method set, update this copy in the
// same commit (see codetaint/AGENTS.md's "Root-side test doubles" section for
// the same rule on the reducer-root copy).
type recordingCodeInterprocEvidenceWriter struct {
	writeCalls      int
	writtenRows     []map[string]any
	writeScopeID    string
	writeEvidence   string
	retractCalls    int
	retractScopeIDs []string
	retractEvidence string
	globalRetracts  int
	globalEvidence  string

	// anchored-delete methods
	retractByUIDsCalls    int
	retractByUIDsUids     []string
	retractByUIDsScopes   []string
	retractByUIDsEvidence string
	sourceByUIDsCalls     int
	sourceByUIDsUids      []string
	sourceByUIDsEvidence  string
	staleByUIDsCalls      int
	staleByUIDsUids       []string
	staleByUIDsScope      string
	staleByUIDsGeneration string
	staleByUIDsEvidence   string
}

func (w *recordingCodeInterprocEvidenceWriter) WriteCodeInterprocEvidence(
	_ context.Context, rows []map[string]any, scopeID, _ string, evidenceSource string,
) error {
	w.writeCalls++
	w.writtenRows = append(w.writtenRows, rows...)
	w.writeScopeID = scopeID
	w.writeEvidence = evidenceSource
	return nil
}

func (w *recordingCodeInterprocEvidenceWriter) RetractCodeInterprocEvidence(
	_ context.Context, scopeIDs []string, _ string, evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return nil
}

func (w *recordingCodeInterprocEvidenceWriter) RetractCodeInterprocEvidenceSource(
	_ context.Context,
	evidenceSource string,
) error {
	w.globalRetracts++
	w.globalEvidence = evidenceSource
	return nil
}

func (w *recordingCodeInterprocEvidenceWriter) RetractCodeInterprocEvidenceByUIDs(
	_ context.Context, sourceUIDs []string, scopeIDs []string, evidenceSource string,
) error {
	w.retractByUIDsCalls++
	w.retractByUIDsUids = append(w.retractByUIDsUids, sourceUIDs...)
	w.retractByUIDsScopes = append(w.retractByUIDsScopes, scopeIDs...)
	w.retractByUIDsEvidence = evidenceSource
	return nil
}

func (w *recordingCodeInterprocEvidenceWriter) RetractCodeInterprocEvidenceSourceByUIDs(
	_ context.Context, sourceUIDs []string, evidenceSource string,
) error {
	w.sourceByUIDsCalls++
	w.sourceByUIDsUids = append(w.sourceByUIDsUids, sourceUIDs...)
	w.sourceByUIDsEvidence = evidenceSource
	return nil
}

func (w *recordingCodeInterprocEvidenceWriter) RetractStaleCodeInterprocEvidenceByUIDs(
	_ context.Context, sourceUIDs []string, scopeID, generationID, evidenceSource string,
) error {
	w.staleByUIDsCalls++
	w.staleByUIDsUids = append(w.staleByUIDsUids, sourceUIDs...)
	w.staleByUIDsScope = scopeID
	w.staleByUIDsGeneration = generationID
	w.staleByUIDsEvidence = evidenceSource
	return nil
}

// stubCodeInterprocEvidenceLoader satisfies codetaint.CodeInterprocEvidenceLoader
// (LoadCodeInterprocEvidence only; this package's fixpoint projector never
// needs the materialization handler's envelope-returning
// CodeInterprocEvidenceFactLoader shape, unlike the reducer root's copy).
type stubCodeInterprocEvidenceLoader struct {
	inputs []codetaint.CodeInterprocEvidenceInput
}

func (l stubCodeInterprocEvidenceLoader) LoadCodeInterprocEvidence(context.Context, string, string) ([]codetaint.CodeInterprocEvidenceInput, error) {
	return l.inputs, nil
}

// sampleCodeInterprocInput mirrors the reducer root's sampleCodeInterprocInput
// (codedataflow_evidence_test_helpers_test.go).
func sampleCodeInterprocInput() codetaint.CodeInterprocEvidenceInput {
	return codetaint.CodeInterprocEvidenceInput{
		SourceFunctionUID: "func-source", SinkFunctionUID: "func-sink",
		RelativePath: "src/handler.go", SourceFunctionName: "readRequest",
		SinkFunctionName: "execQuery", Language: "go", SinkKind: "sql",
		SourceKind: "http_request", Confidence: 0.7, Cloud: true,
	}
}
