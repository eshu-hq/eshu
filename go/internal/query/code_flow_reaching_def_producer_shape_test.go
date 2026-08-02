// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
	"time"
)

// TestReachingDefAnswersFromCollectorPayloadShape is the read half of the
// producer/reader seam issue #5692 left unproven.
//
// Its collector-side mirror,
// collector.TestRealParserDataflowFactCarriesTheReachingDefReadContract, drives
// the real parser with ESHU_EMIT_DATAFLOW on and asserts the emitted
// code_dataflow_function payload carries exactly the keys listed there. This
// asserts the other direction: that a payload of that shape produces a
// reaching-def answer with definitions in it. Neither test alone catches a
// producer and a reader that drifted apart — the producer test would still see
// a fact, and the reader test would still see a well-formed empty answer.
//
// def_use is the field that decides whether the answer says anything. A
// reaching-def response whose definitions carry an empty def_use is
// indistinguishable, to a caller, from a function that genuinely has no
// reaching definitions.
func TestReachingDefAnswersFromCollectorPayloadShape(t *testing.T) {
	t.Parallel()

	// The shape the collector emits, verbatim: the keys its mirror asserts are
	// present, with the JSON-native types a Postgres JSONB round trip yields.
	payload := map[string]any{
		"repo_id":       "repo-alpha",
		"relative_path": "handlers.go",
		"function_name": "Handle",
		"function_uid":  "func-handle",
		"language":      "go",
		"line_number":   float64(5),
		"def_use": []any{
			map[string]any{"binding": "trimmed", "def_line": float64(6), "use_line": float64(10)},
		},
	}

	function := codeFlowFunctionFromPayload(payload, "fact-1", "gen-1", "code_dataflow_function", time.Now())

	if function.FunctionName != "Handle" {
		t.Errorf("FunctionName = %q, want Handle", function.FunctionName)
	}
	if function.RelativePath != "handlers.go" {
		t.Errorf("RelativePath = %q, want handlers.go", function.RelativePath)
	}
	if function.LineNumber != 5 {
		t.Errorf("LineNumber = %d, want 5", function.LineNumber)
	}
	if len(function.DefUse) == 0 {
		t.Fatalf("DefUse is empty: the collector's def_use rows did not survive the read mapping (%+v)", payload["def_use"])
	}

	rows := codeFlowReachingPayloads([]CodeFlowFunction{function})
	if len(rows) != 1 {
		t.Fatalf("codeFlowReachingPayloads() returned %d rows, want 1", len(rows))
	}
	defUse, ok := rows[0]["def_use"].([]map[string]any)
	if !ok || len(defUse) == 0 {
		t.Fatalf("answer def_use = %#v, want the collector's rows: an empty one reads as 'no reaching definitions'", rows[0]["def_use"])
	}
	if got := defUse[0]["binding"]; got != "trimmed" {
		t.Errorf("def_use[0].binding = %v, want trimmed", got)
	}
	if rows[0]["fact_label"] != "exact_parser_fact" {
		t.Errorf("fact_label = %v, want exact_parser_fact", rows[0]["fact_label"])
	}
}
