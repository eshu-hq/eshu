// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
	handler := MaterializationHandler{
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
	handler := MaterializationHandler{
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
