// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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

// stubCodeInterprocEvidenceLoader satisfies BOTH the fixpoint projector's typed
// CodeInterprocEvidenceLoader (returning inputs) and the materialization
// handler's CodeInterprocEvidenceFactLoader (returning envelopes built from the
// same inputs), so the one stub serves both call contexts.
type stubCodeInterprocEvidenceLoader struct {
	inputs []CodeInterprocEvidenceInput
}

func (l stubCodeInterprocEvidenceLoader) LoadCodeInterprocEvidence(context.Context, string, string) ([]CodeInterprocEvidenceInput, error) {
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
// envelope carrying the fields a sample CodeInterprocEvidenceInput decodes to.
func codeInterprocEvidenceEnvelope(in CodeInterprocEvidenceInput) facts.Envelope {
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

func sampleCodeInterprocInput() CodeInterprocEvidenceInput {
	return CodeInterprocEvidenceInput{
		SourceFunctionUID: "func-source", SinkFunctionUID: "func-sink",
		RelativePath: "src/handler.go", SourceFunctionName: "readRequest",
		SinkFunctionName: "execQuery", Language: "go", SinkKind: "sql",
		SourceKind: "http_request", Confidence: 0.7, Cloud: true,
	}
}

// TestCodeInterprocEvidenceHandlerRetractsThenWrites proves the handler retracts
// the prior generation (when one exists) and writes the projected edge rows.
func TestCodeInterprocEvidenceHandlerRetractsThenWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeInterprocEvidenceWriter{}
	handler := CodeInterprocEvidenceMaterializationHandler{
		Loader:               stubCodeInterprocEvidenceLoader{inputs: []CodeInterprocEvidenceInput{sampleCodeInterprocInput()}},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), codeInterprocEvidenceIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 || writer.retractEvidence != codeInterprocEvidenceSource {
		t.Fatalf("retract = %d calls (evidence %q), want 1 with reducer/code-interproc", writer.retractCalls, writer.retractEvidence)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 1 {
		t.Fatalf("write = %d calls, %d rows, want 1/1", writer.writeCalls, len(writer.writtenRows))
	}
	row := writer.writtenRows[0]
	if row["source_function_uid"] != "func-source" || row["sink_function_uid"] != "func-sink" || row["uid"] == "" {
		t.Fatalf("row not projected with source/sink uid + edge uid: %+v", row)
	}
	if result.CanonicalWrites != 1 || result.Status != ResultStatusSucceeded {
		t.Fatalf("result = %+v, want 1 canonical write succeeded", result)
	}
}

// TestCodeInterprocEvidenceHandlerSkipsRetractOnFirstGeneration proves the
// retract is skipped when there is no prior generation.
func TestCodeInterprocEvidenceHandlerSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeInterprocEvidenceWriter{}
	handler := CodeInterprocEvidenceMaterializationHandler{
		Loader:               stubCodeInterprocEvidenceLoader{inputs: []CodeInterprocEvidenceInput{sampleCodeInterprocInput()}},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if _, err := handler.Handle(context.Background(), codeInterprocEvidenceIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retract called %d times on first generation, want 0", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("write = %d calls, want 1", writer.writeCalls)
	}
}

// TestCodeInterprocEvidenceHandlerRejectsWrongDomain proves the handler refuses
// an intent for another domain.
func TestCodeInterprocEvidenceHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := CodeInterprocEvidenceMaterializationHandler{
		Loader: stubCodeInterprocEvidenceLoader{},
		Writer: &recordingCodeInterprocEvidenceWriter{},
	}
	intent := codeInterprocEvidenceIntent()
	intent.Domain = DomainDataLineage
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle accepted a non-interproc domain")
	}
}

// TestExtractCodeInterprocEvidenceRowsDropsUnresolvedAndIsIdempotent proves a
// finding missing either endpoint uid is dropped and the edge uid is stable
// across runs.
func TestExtractCodeInterprocEvidenceRowsDropsUnresolvedAndIsIdempotent(t *testing.T) {
	t.Parallel()

	missingSink := sampleCodeInterprocInput()
	missingSink.SinkFunctionUID = ""
	missingSource := sampleCodeInterprocInput()
	missingSource.SourceFunctionUID = ""
	rows := ExtractCodeInterprocEvidenceRows([]CodeInterprocEvidenceInput{
		sampleCodeInterprocInput(), missingSink, missingSource,
	})
	if len(rows) != 1 {
		t.Fatalf("want 1 row (both unresolved dropped), got %d", len(rows))
	}
	again := ExtractCodeInterprocEvidenceRows([]CodeInterprocEvidenceInput{sampleCodeInterprocInput()})
	if rows[0]["uid"] != again[0]["uid"] {
		t.Fatalf("edge uid not stable across runs: %v vs %v", rows[0]["uid"], again[0]["uid"])
	}
}

// TestExtractCodeInterprocEvidenceRowsCarriesWhyTrailOutsideUID proves the
// ordered why trail is projected as evidence payload without changing edge
// identity.
func TestExtractCodeInterprocEvidenceRowsCarriesWhyTrailOutsideUID(t *testing.T) {
	t.Parallel()

	input := sampleCodeInterprocInput()
	input.WhyTrail = []map[string]any{
		{"role": "source", "function_uid": "func-source", "slot_kind": "param", "slot_index": 0},
		{"role": "sink", "function_uid": "func-sink", "slot_kind": "return"},
	}
	input.WhyTrailTruncated = true

	row := ExtractCodeInterprocEvidenceRows([]CodeInterprocEvidenceInput{input})[0]
	if row["why_trail_truncated"] != true {
		t.Fatalf("why_trail_truncated not carried: %+v", row)
	}
	if row["why_trail_json"] == "" {
		t.Fatalf("why_trail_json missing: %+v", row)
	}
	if _, ok := row["why_trail"]; ok {
		t.Fatalf("raw why_trail should not be sent to graph writer rows: %+v", row)
	}

	withoutTrail := sampleCodeInterprocInput()
	if got := ExtractCodeInterprocEvidenceRows([]CodeInterprocEvidenceInput{withoutTrail})[0]["uid"]; got != row["uid"] {
		t.Fatalf("trail changed edge uid: with=%v without=%v", row["uid"], got)
	}
}
