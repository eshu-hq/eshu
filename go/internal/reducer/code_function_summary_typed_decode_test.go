// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// TestCodeFunctionSummaryEffectsMapsAllFields proves the JSONB float64/any
// payload shapes decode through the typed contracts seam into summary.Effects
// (Contract System v1 Wave 4f S2, issue #4754).
func TestCodeFunctionSummaryEffectsMapsAllFields(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeFunctionSummaryFactKind,
		Payload: map[string]any{
			"function_id":      "repo-1:pkg::view",
			"param_to_return":  []any{float64(0), float64(2)},
			"source_to_return": []any{"http_request"},
			"param_to_sink":    []any{map[string]any{"param": float64(1), "sink_kind": "sql"}},
			"param_to_call_arg": []any{map[string]any{
				"callee": "repo-1:pkg::query", "param": float64(0), "arg": float64(1),
			}},
		},
	}

	id, effects, ok, err := codeFunctionSummaryEffects(envelope)
	if err != nil {
		t.Fatalf("codeFunctionSummaryEffects error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("codeFunctionSummaryEffects ok = false, want true")
	}
	if id != summary.FunctionID("repo-1:pkg::view") {
		t.Fatalf("codeFunctionSummaryEffects id = %q, want repo-1:pkg::view", id)
	}
	if len(effects.ParamToReturn) != 2 || effects.ParamToReturn[1] != 2 {
		t.Fatalf("param_to_return not coerced: %+v", effects.ParamToReturn)
	}
	if len(effects.ParamToSink) != 1 || effects.ParamToSink[0].Param != 1 || effects.ParamToSink[0].SinkKind != "sql" {
		t.Fatalf("param_to_sink not coerced: %+v", effects.ParamToSink)
	}
	if len(effects.SourceToReturn) != 1 || effects.SourceToReturn[0] != "http_request" {
		t.Fatalf("source_to_return not coerced: %+v", effects.SourceToReturn)
	}
	if len(effects.ParamToCallArg) != 1 || effects.ParamToCallArg[0].Callee != summary.FunctionID("repo-1:pkg::query") || effects.ParamToCallArg[0].Arg != 1 {
		t.Fatalf("param_to_call_arg not coerced: %+v", effects.ParamToCallArg)
	}
}

// TestCodeFunctionSummaryEffectsTrimsCallee proves the Codex review fix
// (PR #4758): a padded param_to_call_arg[].callee is TrimSpace'd before it
// becomes a summary.FunctionID, so the durable summary keys its callee edge on
// the same trimmed FunctionID the fixpoint's summary/graph-id maps use. The
// old loader trimmed via payloadString; the typed path must trim explicitly or
// the fixpoint cannot match the callee summary it previously matched.
func TestCodeFunctionSummaryEffectsTrimsCallee(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeFunctionSummaryFactKind,
		Payload: map[string]any{
			"function_id": "repo-1:pkg::view",
			"param_to_call_arg": []any{map[string]any{
				"callee": "  repo-1:pkg::query  ", "param": float64(0), "arg": float64(1),
			}},
		},
	}

	_, effects, ok, err := codeFunctionSummaryEffects(envelope)
	if err != nil || !ok {
		t.Fatalf("codeFunctionSummaryEffects err=%v ok=%v, want nil/true", err, ok)
	}
	if len(effects.ParamToCallArg) != 1 || effects.ParamToCallArg[0].Callee != summary.FunctionID("repo-1:pkg::query") {
		t.Fatalf("callee not trimmed: %+v", effects.ParamToCallArg)
	}
}

// TestCodeFunctionSummaryEffectsMissingFunctionIDReturnsError proves a
// code_function_summary fact missing its required function_id RETURNS a decode
// error (routed to an input_invalid dead-letter by
// ExtractCodeFunctionSummaryEffectsWithQuarantine), rather than being silently
// dropped.
func TestCodeFunctionSummaryEffectsMissingFunctionIDReturnsError(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeFunctionSummaryFactKind,
		Payload:  map[string]any{"repo_id": "repo-1"},
	}
	if _, _, _, err := codeFunctionSummaryEffects(envelope); err == nil {
		t.Fatal("codeFunctionSummaryEffects(missing function_id) error = nil, want a classified input_invalid error")
	}
}

// TestCodeFunctionSummaryEffectsBlankFunctionIDReturnsNotOKNoError proves a
// present-but-blank function_id (decode succeeds, but identity is empty)
// returns ok=false WITHOUT an error: it is a valid-but-empty observation the
// pre-Contract-System `if id == "" { continue }` guard dropped, not a
// malformed payload, so it is skipped rather than dead-lettered.
func TestCodeFunctionSummaryEffectsBlankFunctionIDReturnsNotOKNoError(t *testing.T) {
	t.Parallel()

	for _, functionID := range []string{"", "   "} {
		envelope := facts.Envelope{
			FactKind: facts.CodeFunctionSummaryFactKind,
			Payload:  map[string]any{"function_id": functionID, "repo_id": "repo-1"},
		}
		_, _, ok, err := codeFunctionSummaryEffects(envelope)
		if err != nil {
			t.Fatalf("codeFunctionSummaryEffects(function_id=%q) error = %v, want nil (blank is a drop, not a decode error)", functionID, err)
		}
		if ok {
			t.Fatalf("codeFunctionSummaryEffects(function_id=%q) ok = true, want false", functionID)
		}
	}
}

// TestCodeFunctionGraphIDMapsFields proves the graph-uid mapping decodes both
// the function id and the resolved graph uid, and that an unresolved graph uid
// decodes to an empty string (not an error) so the replacement writer can
// clear stale mappings.
func TestCodeFunctionGraphIDMapsFields(t *testing.T) {
	t.Parallel()

	resolved := facts.Envelope{
		FactKind: facts.CodeFunctionSummaryFactKind,
		Payload: map[string]any{
			"function_id": "repo-1:pkg::view",
			"graph_uid":   "uid:view-fn",
		},
	}
	id, uid, ok, err := codeFunctionGraphID(resolved)
	if err != nil || !ok || id != summary.FunctionID("repo-1:pkg::view") || uid != "uid:view-fn" {
		t.Fatalf("codeFunctionGraphID(resolved) = (%q, %q, %v, %v), want (repo-1:pkg::view, uid:view-fn, true, nil)", id, uid, ok, err)
	}

	unresolved := facts.Envelope{
		FactKind: facts.CodeFunctionSummaryFactKind,
		Payload:  map[string]any{"function_id": "repo-1:pkg::orphan"},
	}
	id, uid, ok, err = codeFunctionGraphID(unresolved)
	if err != nil || !ok || uid != "" {
		t.Fatalf("codeFunctionGraphID(unresolved) = (%q, %q, %v, %v), want (repo-1:pkg::orphan, \"\", true, nil)", id, uid, ok, err)
	}
}

// TestCodeFunctionSourceMapsFields proves the interproc.Source port mapping
// decodes function_id, param_index, and kind, including the JSONB float64
// param_index coercion.
func TestCodeFunctionSourceMapsFields(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeFunctionSourceFactKind,
		Payload: map[string]any{
			"function_id": "repo-1:pkg::handle",
			"param_index": float64(2),
			"kind":        "http_request",
		},
	}
	source, ok, err := codeFunctionSource(envelope)
	if err != nil || !ok {
		t.Fatalf("codeFunctionSource err=%v ok=%v, want nil/true", err, ok)
	}
	if string(source.Port.Func) != "repo-1:pkg::handle" || source.Port.Slot.Index != 2 || source.Kind != "http_request" {
		t.Fatalf("codeFunctionSource = %+v, want Func=repo-1:pkg::handle Index=2 Kind=http_request", source)
	}
}

// TestCodeFunctionSourceMissingKindReturnsError proves a code_function_source
// fact missing its required kind RETURNS a decode error (routed to an
// input_invalid dead-letter), not a silent drop.
func TestCodeFunctionSourceMissingKindReturnsError(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeFunctionSourceFactKind,
		Payload: map[string]any{
			"function_id": "repo-1:pkg::handle",
			"param_index": float64(0),
		},
	}
	if _, _, err := codeFunctionSource(envelope); err == nil {
		t.Fatal("codeFunctionSource(missing kind) error = nil, want a classified input_invalid error")
	}
}

// TestCodeFunctionSourceBlankFieldsReturnNotOKNoError proves a present-but-blank
// function_id or kind returns ok=false WITHOUT an error (a valid-but-empty
// observation the pre-Contract-System guard dropped, not a malformed payload).
func TestCodeFunctionSourceBlankFieldsReturnNotOKNoError(t *testing.T) {
	t.Parallel()

	blankFunctionID := facts.Envelope{
		FactKind: facts.CodeFunctionSourceFactKind,
		Payload: map[string]any{
			"function_id": "   ",
			"kind":        "http_request",
			"param_index": float64(0),
		},
	}
	if _, ok, err := codeFunctionSource(blankFunctionID); err != nil || ok {
		t.Fatalf("codeFunctionSource(blank function_id) err=%v ok=%v, want nil/false", err, ok)
	}

	blankKind := facts.Envelope{
		FactKind: facts.CodeFunctionSourceFactKind,
		Payload: map[string]any{
			"function_id": "repo-1:pkg::handle",
			"kind":        "",
			"param_index": float64(0),
		},
	}
	if _, ok, err := codeFunctionSource(blankKind); err != nil || ok {
		t.Fatalf("codeFunctionSource(blank kind) err=%v ok=%v, want nil/false", err, ok)
	}
}
