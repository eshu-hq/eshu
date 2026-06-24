// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// TestCodeFunctionSummaryEffectsFromPayload proves the JSONB float64/any shapes
// are coerced back into typed summary.Effects.
func TestCodeFunctionSummaryEffectsFromPayload(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"function_id":      "repo-1\x1fpkg\x1f\x1fview",
		"param_to_return":  []any{float64(0), float64(2)},
		"source_to_return": []any{"http_request"},
		"param_to_sink":    []any{map[string]any{"param": float64(1), "sink_kind": "sql"}},
		"param_to_call_arg": []any{map[string]any{
			"callee": "repo-1\x1fpkg\x1f\x1fquery", "param": float64(0), "arg": float64(1),
		}},
	}
	effects := codeFunctionSummaryEffectsFromPayload(payload)
	if len(effects.ParamToReturn) != 2 || effects.ParamToReturn[1] != 2 {
		t.Fatalf("param_to_return not coerced: %+v", effects.ParamToReturn)
	}
	if len(effects.ParamToSink) != 1 || effects.ParamToSink[0].Param != 1 || effects.ParamToSink[0].SinkKind != "sql" {
		t.Fatalf("param_to_sink not coerced: %+v", effects.ParamToSink)
	}
	if len(effects.SourceToReturn) != 1 || effects.SourceToReturn[0] != "http_request" {
		t.Fatalf("source_to_return not coerced: %+v", effects.SourceToReturn)
	}
	if len(effects.ParamToCallArg) != 1 || effects.ParamToCallArg[0].Callee != summary.FunctionID("repo-1\x1fpkg\x1f\x1fquery") || effects.ParamToCallArg[0].Arg != 1 {
		t.Fatalf("param_to_call_arg not coerced: %+v", effects.ParamToCallArg)
	}
}
