// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestExtractCodeCallRowsResolvesKotlinClassCallerFromRealParse proves #7686
// at the reducer level: a call inside a Kotlin class body but outside any
// function (a property initializer) resolves to that class as its caller,
// using the spans the Kotlin parser actually emits. Before the fix the class
// span was a single line, so the in-class call fell outside its caller's span
// and the row was dropped. The top-level property call in the same file has
// no containing span, so it emits no row instead of borrowing a caller.
func TestExtractCodeCallRowsResolvesKotlinClassCallerFromRealParse(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "worker.kt")
	if err := os.WriteFile(callerPath, []byte(`fun helper(): Int = 1

class Worker {
    val value: Int = helper()
}

val top = helper()
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	if classes, ok := callerPayload["classes"].([]map[string]any); ok {
		for _, class := range classes {
			if name, _ := class["name"].(string); name == "Worker" {
				class["uid"] = "content-entity:kotlin-worker"
			}
		}
	}
	if functions, ok := callerPayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			if name, _ := function["name"].(string); name == "helper" {
				function["uid"] = "content-entity:kotlin-helper"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "worker.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:kotlin-worker"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-helper"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}
