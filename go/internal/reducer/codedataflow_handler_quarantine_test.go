// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	codetaint "github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
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
	handler := codetaint.CodeTaintEvidenceMaterializationHandler{
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
	handler := codetaint.CodeInterprocEvidenceMaterializationHandler{
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

// The function-summary handler's quarantine proofs
// (TestCodeFunctionSummaryHandlerQuarantinesMalformedFact and
// TestCodeFunctionSummaryHandlerQuarantinesMalformedSourceFact) moved to
// internal/reducer/code/function/summary/quarantine_test.go with the rest of
// that family (issue #6061).
