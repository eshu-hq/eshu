// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCodeTaintEvidenceHandlerQuarantinesMalformedFact is the coordinator-
// required production-path proof for Wave 4f S2 (issue #4754, epic #4566 §1):
// a code_taint_evidence fact missing its required function_uid, fed through the
// ACTUAL loader -> handler path, must be recorded as an input_invalid
// quarantine (Result.SubSignals["input_invalid_facts"] == 1) rather than
// silently dropped, while a valid sibling still projects its evidence row. The
// earlier wrapper-only test (TestDecodeCodeTaintEvidenceInputMissingFunctionUIDReturnsError)
// proves the decode returns the error; THIS proves the handler acts on it.
func TestCodeTaintEvidenceHandlerQuarantinesMalformedFact(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "taint-malformed",
		FactKind: facts.CodeTaintEvidenceFactKind,
		Payload: map[string]any{
			// "function_uid" intentionally absent.
			"relative_path": "src/handler.go",
			"kind":          "sql_injection",
		},
	}
	valid := codeTaintEvidenceEnvelope(sampleCodeTaintInput())

	writer := &recordingCodeTaintEvidenceWriter{}
	handler := CodeTaintEvidenceMaterializationHandler{
		Loader:               stubCodeTaintEvidenceLoader{envelopes: []facts.Envelope{malformed, valid}},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), codeTaintEvidenceIntent())
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed taint fact must be quarantined per-fact, not fail the whole intent", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-function_uid fact must be recorded as one input_invalid quarantine", got)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 1 || writer.writtenRows[0]["function_uid"] != "func-handle" {
		t.Fatalf("valid sibling not projected despite the malformed quarantine: %+v", writer.writtenRows)
	}
}

// TestCodeInterprocEvidenceHandlerQuarantinesMalformedFact mirrors the taint
// production-path proof for the interproc family's edge endpoints.
func TestCodeInterprocEvidenceHandlerQuarantinesMalformedFact(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "interproc-malformed",
		FactKind: facts.CodeInterprocEvidenceFactKind,
		Payload: map[string]any{
			// "source_function_uid" intentionally absent.
			"sink_function_uid": "uid:sink",
			"sink_kind":         "sql_exec",
		},
	}
	valid := codeInterprocEvidenceEnvelope(sampleCodeInterprocInput())

	writer := &recordingCodeInterprocEvidenceWriter{}
	handler := CodeInterprocEvidenceMaterializationHandler{
		Loader:               stubInterprocFactLoader{envelopes: []facts.Envelope{malformed, valid}},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), codeInterprocEvidenceIntent())
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed interproc fact must be quarantined per-fact, not fail the whole intent", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-source_function_uid fact must be recorded as one input_invalid quarantine", got)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 1 {
		t.Fatalf("valid sibling not projected despite the malformed quarantine: %+v", writer.writtenRows)
	}
}

// stubInterprocFactLoader returns a fixed envelope batch for the interproc
// handler's fact-loader path.
type stubInterprocFactLoader struct {
	envelopes []facts.Envelope
}

func (l stubInterprocFactLoader) LoadCodeInterprocEvidenceFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

// TestCodeFunctionSummaryHandlerQuarantinesMalformedFact proves the
// function-summary handler records an input_invalid quarantine for a
// code_function_summary fact missing its required function_id, while a valid
// sibling still persists.
func TestCodeFunctionSummaryHandlerQuarantinesMalformedFact(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "summary-malformed",
		FactKind: facts.CodeFunctionSummaryFactKind,
		Payload: map[string]any{
			// "function_id" intentionally absent.
			"graph_uid": "uid:orphan",
		},
	}
	valid := facts.Envelope{
		FactID:   "summary-valid",
		FactKind: facts.CodeFunctionSummaryFactKind,
		Payload: map[string]any{
			"function_id": "repo-1\x1fpkg\x1f\x1fview",
			"graph_uid":   "uid:view",
		},
	}

	writer := &recordingCodeFunctionSummaryWriter{}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubSummaryFactLoader{envelopes: []facts.Envelope{malformed, valid}},
		Writer: writer,
	}

	result, err := handler.Handle(context.Background(), codeFunctionSummaryIntent())
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed summary fact must be quarantined per-fact, not fail the whole intent", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-function_id fact must be recorded as one input_invalid quarantine", got)
	}
	if writer.upsertCalls != 1 || len(writer.snapshot.Functions) != 1 {
		t.Fatalf("valid sibling summary not persisted despite the malformed quarantine: %+v", writer.snapshot)
	}
}

// stubSummaryFactLoader returns a fixed code_function_summary envelope batch
// for the function-summary handler's summary/graph-id fact-loader paths.
type stubSummaryFactLoader struct {
	envelopes []facts.Envelope
}

func (l stubSummaryFactLoader) LoadCodeFunctionSummaryFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

// TestCodeFunctionSummaryHandlerQuarantinesMalformedSourceFact proves the
// source loader path also records an input_invalid quarantine for a malformed
// code_function_source fact.
func TestCodeFunctionSummaryHandlerQuarantinesMalformedSourceFact(t *testing.T) {
	t.Parallel()

	malformedSource := facts.Envelope{
		FactID:   "source-malformed",
		FactKind: facts.CodeFunctionSourceFactKind,
		Payload: map[string]any{
			// "kind" intentionally absent.
			"function_id": "repo-1\x1fpkg\x1f\x1fhandle",
			"param_index": float64(0),
		},
	}

	summaryWriter := &recordingCodeFunctionSummaryWriter{}
	srcWriter := &recordingCodeFunctionSourceWriter{}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader:       stubSummaryFactLoader{},
		Writer:       summaryWriter,
		SourceLoader: stubSourceFactLoader{envelopes: []facts.Envelope{malformedSource}},
		SourceWriter: srcWriter,
	}

	result, err := handler.Handle(context.Background(), codeFunctionSummaryIntent())
	if err != nil {
		t.Fatalf("Handle returned error %v; a malformed source fact must be quarantined per-fact", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-kind source fact must be recorded as one input_invalid quarantine", got)
	}
}

// stubSourceFactLoader returns a fixed code_function_source envelope batch.
type stubSourceFactLoader struct {
	envelopes []facts.Envelope
}

func (l stubSourceFactLoader) LoadCodeFunctionSourceFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}
