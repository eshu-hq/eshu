// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
)

// This file holds the reducer root's own copies of test doubles the moved
// taint package's tests also define
// (go/internal/reducer/code/taint/evidence_materialization_test.go,
// code/taint/interproc_evidence_materialization_test.go, and
// code/taint/interproc_projected_edge_backfill_test.go). Before issue #6061 moved
// the code_taint_evidence/code_interproc_evidence family out of this package,
// a single set of unexported test doubles served both the family's own tests
// and the root tests that exercise it through DefaultHandlers and
// ValueFlowFixpointEvidenceLoader/Projector, and (for the
// fakeBackfillStateMarker/splitPipeKey/stringSlicesEqual cluster) the sibling
// projected_source_edge_backfill family that also stayed in root. Go test
// files cannot share unexported symbols across packages, so the split needs
// its own copy on each side; keep the two in sync by hand if either changes
// shape. The value-flow stale-cleanup runner's own copy of the interproc
// ledger fake moved with it to code/value/cleanup/runner_test.go (#6061).

// recordingCodeTaintEvidenceWriter satisfies taint.EvidenceWriter.
type recordingCodeTaintEvidenceWriter struct {
	writeCalls      int
	writtenRows     []map[string]any
	writeScopeID    string
	writeEvidence   string
	retractCalls    int
	retractScopeIDs []string
	retractEvidence string
}

func (w *recordingCodeTaintEvidenceWriter) WriteCodeTaintEvidence(
	_ context.Context, rows []map[string]any, scopeID, _ string, evidenceSource string,
) error {
	w.writeCalls++
	w.writtenRows = append(w.writtenRows, rows...)
	w.writeScopeID = scopeID
	w.writeEvidence = evidenceSource
	return nil
}

func (w *recordingCodeTaintEvidenceWriter) RetractCodeTaintEvidence(
	_ context.Context, scopeIDs []string, _ string, evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return nil
}

func (w *recordingCodeTaintEvidenceWriter) RetractCodeTaintEvidenceByUIDs(
	_ context.Context, _ []string, _ []string, _ string,
) error {
	return nil
}

func (w *recordingCodeTaintEvidenceWriter) RetractStaleCodeTaintEvidenceByUIDs(
	_ context.Context, _ []string, _, _, _ string,
) error {
	return nil
}

// stubCodeTaintEvidenceLoader satisfies taint.EvidenceLoader,
// returning raw code_taint_evidence envelopes for the handler to decode.
type stubCodeTaintEvidenceLoader struct {
	envelopes []facts.Envelope
}

func (l stubCodeTaintEvidenceLoader) LoadCodeTaintEvidence(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

// codeTaintEvidenceEnvelope builds a valid code_taint_evidence fact envelope
// carrying the fields a sample taint.EvidenceInput decodes to.
func codeTaintEvidenceEnvelope(in taint.EvidenceInput) facts.Envelope {
	return facts.Envelope{
		FactID:   "taint:" + in.FunctionUID,
		FactKind: facts.CodeTaintEvidenceFactKind,
		Payload: map[string]any{
			"function_uid":  in.FunctionUID,
			"function_name": in.FunctionName,
			"relative_path": in.RelativePath,
			"language":      in.Language,
			"kind":          in.Kind,
			"sink_kind":     in.SinkKind,
			"source_kind":   in.SourceKind,
			"binding":       in.Binding,
			"source_line":   float64(in.SourceLine),
			"sink_line":     float64(in.SinkLine),
			"confidence":    in.Confidence,
			"guard_reason":  in.GuardReason,
		},
	}
}

func codeTaintEvidenceIntent() Intent {
	return Intent{
		IntentID:     "intent-taint-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainCodeTaintEvidence,
	}
}

func sampleCodeTaintInput() taint.EvidenceInput {
	return taint.EvidenceInput{
		FunctionUID: "func-handle", FunctionName: "handle", RelativePath: "src/handler.go",
		Language: "go", Kind: "TAINTED", SinkKind: "sql", SourceKind: "http_request",
		Binding: "q", SourceLine: 4, SinkLine: 5, Confidence: 0.8, GuardReason: "allowed",
	}
}

// recordingCodeInterprocEvidenceWriter satisfies
// taint.InterprocEvidenceWriter.
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

// stubCodeInterprocEvidenceLoader satisfies BOTH the fixpoint projector's
// typed taint.InterprocEvidenceLoader (returning inputs) and the
// materialization handler's taint.InterprocEvidenceFactLoader
// (returning envelopes built from the same inputs), so the one stub serves
// both call contexts.
type stubCodeInterprocEvidenceLoader struct {
	inputs []taint.InterprocEvidenceInput
}

func (l stubCodeInterprocEvidenceLoader) LoadCodeInterprocEvidence(context.Context, string, string) ([]taint.InterprocEvidenceInput, error) {
	return l.inputs, nil
}

func (l stubCodeInterprocEvidenceLoader) LoadCodeInterprocEvidenceFacts(context.Context, string, string) ([]facts.Envelope, error) {
	envelopes := make([]facts.Envelope, 0, len(l.inputs))
	for _, in := range l.inputs {
		envelopes = append(envelopes, codeInterprocEvidenceEnvelope(in))
	}
	return envelopes, nil
}

// codeInterprocEvidenceEnvelope builds a valid code_interproc_evidence fact
// envelope carrying the fields a sample taint.InterprocEvidenceInput
// decodes to.
func codeInterprocEvidenceEnvelope(in taint.InterprocEvidenceInput) facts.Envelope {
	payload := map[string]any{
		"source_function_uid":  in.SourceFunctionUID,
		"sink_function_uid":    in.SinkFunctionUID,
		"relative_path":        in.RelativePath,
		"source_function_name": in.SourceFunctionName,
		"sink_function_name":   in.SinkFunctionName,
		"language":             in.Language,
		"sink_kind":            in.SinkKind,
		"source_kind":          in.SourceKind,
		"confidence":           in.Confidence,
	}
	if in.Cloud {
		payload["cloud"] = true
	}
	return facts.Envelope{
		FactID:   "interproc:" + in.SourceFunctionUID + ":" + in.SinkFunctionUID,
		FactKind: facts.CodeInterprocEvidenceFactKind,
		Payload:  payload,
	}
}

func codeInterprocEvidenceIntent() Intent {
	return Intent{
		IntentID:     "intent-interproc-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainCodeInterprocEvidence,
	}
}

func sampleCodeInterprocInput() taint.InterprocEvidenceInput {
	return taint.InterprocEvidenceInput{
		SourceFunctionUID: "func-source", SinkFunctionUID: "func-sink",
		RelativePath: "src/handler.go", SourceFunctionName: "readRequest",
		SinkFunctionName: "execQuery", Language: "go", SinkKind: "sql",
		SourceKind: "http_request", Confidence: 0.7, Cloud: true,
	}
}

// fakeBackfillStateMarker satisfies CodeValueFlowBackfillStateMarker (root's
// own copy — compat_projection.go, shared by this file's
// projected_source_edge_backfill_test.go and taint's own copy of this
// same fake in code/taint/interproc_projected_edge_backfill_test.go).
type fakeBackfillStateMarker struct {
	complete     map[string]bool
	markComplete map[string]time.Time
}

func newFakeBackfillStateMarker() *fakeBackfillStateMarker {
	return &fakeBackfillStateMarker{
		complete:     make(map[string]bool),
		markComplete: make(map[string]time.Time),
	}
}

func (m *fakeBackfillStateMarker) IsComplete(_ context.Context, key string) (bool, error) {
	return m.complete[key], nil
}

func (m *fakeBackfillStateMarker) MarkComplete(_ context.Context, key string, at time.Time) error {
	m.markComplete[key] = at
	return nil
}

// splitPipeKey splits a "|"-delimited fake-store key into up to 3 parts.
func splitPipeKey(key string) [3]string {
	var parts [3]string
	si := 0
	for i := 0; i < 3; i++ {
		next := indexByteIn(key, '|', si)
		if next < 0 {
			parts[i] = key[si:]
			return parts
		}
		parts[i] = key[si:next]
		si = next + 1
	}
	return parts
}

func indexByteIn(s string, b byte, start int) int {
	for i := start; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// stringSlicesEqual reports whether two string slices hold the same values
// in the same order.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
