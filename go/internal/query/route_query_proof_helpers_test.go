// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// These helpers back both TestHandleRouteToCallerResolvesJavaSpringHandler
// (code_route_to_caller_java_test.go) and the per-language route
// query-proof matrix (route_query_proof_matrix_test.go, #5361). They were
// originally written Java-only-named inside the Java test file; #5361's
// codex P1 review found the *other* seven route-truth languages' read_surfaces
// ledger citations were unproven against real parser output, so this file
// generalizes them (same behavior, language-neutral names) rather than
// duplicating per-language copies.

// parseRouteFixtureFileForQueryProof runs the real parser over one route
// fixture file under tests/fixtures/ecosystems/<ecosystemDir>/ and returns its
// parsed_file_data payload plus the relative path a file envelope must carry.
func parseRouteFixtureFileForQueryProof(t *testing.T, ecosystemDir, relPath string) (map[string]any, string) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// This file lives at <repoRoot>/go/internal/query/.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "tests", "fixtures", "ecosystems", ecosystemDir)
	sourcePath := filepath.Join(repoRoot, relPath)
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
	}
	relativePath, err := filepath.Rel(repoRoot, sourcePath)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q) error = %v, want nil", repoRoot, sourcePath, err)
	}
	return payload, relativePath
}

// assignQueryProofFunctionUID stamps a synthetic content-entity uid onto the
// real parsed function named name, standing in for the content-entity
// resolution stage that runs downstream of parsing in production. It fails
// the test if no function named name was parsed, so a fixture that stops
// emitting the expected function is caught immediately rather than silently
// producing zero intents for the wrong reason.
func assignQueryProofFunctionUID(t *testing.T, payload map[string]any, name string, uid string) {
	t.Helper()
	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload functions = %T, want []map[string]any", payload["functions"])
	}
	for i := range functions {
		if functions[i]["name"] == name {
			functions[i]["uid"] = uid
			return
		}
	}
	t.Fatalf("payload missing function %q in %#v", name, functions)
}

// queryProofFunctionFields reads the real parsed name/lang/line_number/end_line
// fields for the function named name, so a fake graph row's handler_name,
// handler_language, handler_start_line, and handler_end_line are derived from
// the same parse the reducer intent came from, not invented separately.
func queryProofFunctionFields(t *testing.T, payload map[string]any, name string) (string, string, int, int) {
	t.Helper()
	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload functions = %T, want []map[string]any", payload["functions"])
	}
	for _, fn := range functions {
		if fn["name"] != name {
			continue
		}
		lang, _ := fn["lang"].(string)
		startLine, _ := fn["line_number"].(int)
		endLine, _ := fn["end_line"].(int)
		return name, lang, startLine, endLine
	}
	t.Fatalf("payload missing function %q in %#v", name, functions)
	return "", "", 0, 0
}

// jsonRoundTripQueryProofPayload round-trips a parsed_file_data payload
// through encoding/json, the same production-realistic shape a file fact
// envelope carries on the wire -- []map[string]string route_entries only
// decode through mapSlice() after this round-trip turns them into []any of
// map[string]any.
func jsonRoundTripQueryProofPayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(parsed_file_data) error = %v, want nil", err)
	}
	var roundTripped map[string]any
	if err := json.Unmarshal(raw, &roundTripped); err != nil {
		t.Fatalf("json.Unmarshal(parsed_file_data) error = %v, want nil", err)
	}
	return roundTripped
}

// findQueryProofIntentByFunctionEntityID returns the HANDLES_ROUTE intent
// whose function_entity_id matches entityID.
func findQueryProofIntentByFunctionEntityID(
	intents []reducer.SharedProjectionIntentRow, entityID string,
) (reducer.SharedProjectionIntentRow, bool) {
	for _, intent := range intents {
		if id, _ := intent.Payload["function_entity_id"].(string); id == entityID {
			return intent, true
		}
	}
	return reducer.SharedProjectionIntentRow{}, false
}
