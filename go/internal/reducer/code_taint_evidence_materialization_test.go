// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
)

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

type stubCodeTaintEvidenceLoader struct {
	inputs []CodeTaintEvidenceInput
}

func (l stubCodeTaintEvidenceLoader) LoadCodeTaintEvidence(context.Context, string, string) ([]CodeTaintEvidenceInput, error) {
	return l.inputs, nil
}

func codeTaintEvidenceIntent() Intent {
	return Intent{
		IntentID:     "intent-taint-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainCodeTaintEvidence,
	}
}

func sampleCodeTaintInput() CodeTaintEvidenceInput {
	return CodeTaintEvidenceInput{
		FunctionUID: "func-handle", FunctionName: "handle", RelativePath: "src/handler.go",
		Language: "go", Kind: "TAINTED", SinkKind: "sql", SourceKind: "http_request",
		Binding: "q", SourceLine: 4, SinkLine: 5, Confidence: 0.8, GuardReason: "allowed",
	}
}

// TestCodeTaintEvidenceHandlerRetractsThenWrites proves the handler retracts the
// prior generation (when one exists) and writes the projected rows.
func TestCodeTaintEvidenceHandlerRetractsThenWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeTaintEvidenceWriter{}
	handler := CodeTaintEvidenceMaterializationHandler{
		Loader:               stubCodeTaintEvidenceLoader{inputs: []CodeTaintEvidenceInput{sampleCodeTaintInput()}},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), codeTaintEvidenceIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 || writer.retractEvidence != codeTaintEvidenceSource {
		t.Fatalf("retract = %d calls (evidence %q), want 1 with reducer/code-taint", writer.retractCalls, writer.retractEvidence)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 1 {
		t.Fatalf("write = %d calls, %d rows, want 1/1", writer.writeCalls, len(writer.writtenRows))
	}
	if writer.writtenRows[0]["function_uid"] != "func-handle" || writer.writtenRows[0]["uid"] == "" {
		t.Fatalf("row not projected with function uid + node uid: %+v", writer.writtenRows[0])
	}
	if writer.writtenRows[0]["guard_reason"] != "allowed" {
		t.Fatalf("guard reason not projected: %+v", writer.writtenRows[0])
	}
	if result.CanonicalWrites != 1 || result.Status != ResultStatusSucceeded {
		t.Fatalf("result = %+v, want 1 canonical write succeeded", result)
	}
}

// TestCodeTaintEvidenceHandlerSkipsRetractOnFirstGeneration proves the retract is
// skipped when there is no prior generation.
func TestCodeTaintEvidenceHandlerSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeTaintEvidenceWriter{}
	handler := CodeTaintEvidenceMaterializationHandler{
		Loader:               stubCodeTaintEvidenceLoader{inputs: []CodeTaintEvidenceInput{sampleCodeTaintInput()}},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if _, err := handler.Handle(context.Background(), codeTaintEvidenceIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retract called %d times on first generation, want 0", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("write = %d calls, want 1", writer.writeCalls)
	}
}

// TestCodeTaintEvidenceHandlerRejectsWrongDomain proves the handler refuses an
// intent for another domain.
func TestCodeTaintEvidenceHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := CodeTaintEvidenceMaterializationHandler{
		Loader: stubCodeTaintEvidenceLoader{},
		Writer: &recordingCodeTaintEvidenceWriter{},
	}
	intent := codeTaintEvidenceIntent()
	intent.Domain = DomainDataLineage
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle accepted a non-taint domain")
	}
}

// TestExtractCodeTaintEvidenceRowsDropsUnresolvedAndIsIdempotent proves a finding
// without a Function uid is dropped and the node uid is stable across runs.
func TestExtractCodeTaintEvidenceRowsDropsUnresolvedAndIsIdempotent(t *testing.T) {
	t.Parallel()

	unresolved := sampleCodeTaintInput()
	unresolved.FunctionUID = ""
	rows := ExtractCodeTaintEvidenceRows([]CodeTaintEvidenceInput{sampleCodeTaintInput(), unresolved})
	if len(rows) != 1 {
		t.Fatalf("want 1 row (unresolved dropped), got %d", len(rows))
	}
	again := ExtractCodeTaintEvidenceRows([]CodeTaintEvidenceInput{sampleCodeTaintInput()})
	if rows[0]["uid"] != again[0]["uid"] {
		t.Fatalf("node uid not stable across runs: %v vs %v", rows[0]["uid"], again[0]["uid"])
	}
}
