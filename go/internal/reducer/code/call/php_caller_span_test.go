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

// TestExtractCodeCallRowsResolvesPHPMethodCallerFromRealParse proves #7641 at
// the reducer level: a call inside a PHP method resolves to that method as its
// caller, using the spans the PHP parser actually emits. Before the fix every
// PHP declaration span was a single line, so the in-method call fell outside
// its caller's span and the row was dropped.
func TestExtractCodeCallRowsResolvesPHPMethodCallerFromRealParse(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "worker.php")
	if err := os.WriteFile(callerPath, []byte(`<?php
function helper() {
    return 1;
}

class Worker {
    public function run() {
        return helper();
    }
}

helper();
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
	if functions, ok := callerPayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			name, _ := function["name"].(string)
			classContext, _ := function["class_context"].(string)
			switch {
			case name == "run" && classContext == "Worker":
				function["uid"] = "content-entity:php-worker-run"
			case name == "helper":
				function["uid"] = "content-entity:php-helper"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-php",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-php",
				"relative_path":    "worker.php",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:php-worker-run"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:php-helper"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

// TestExtractCodeCallRowsDropsTopLevelPHPCallWithoutBorrowingACaller is the
// #7641 negative case: a top-level PHP call has no containing span in its own
// file, so it emits no row instead of borrowing a caller from another function
// (#7640). The positive test above asserts the same file yields exactly the
// in-method row; this test pins the top-level call alone.
func TestExtractCodeCallRowsDropsTopLevelPHPCallWithoutBorrowingACaller(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "top.php")
	if err := os.WriteFile(callerPath, []byte(`<?php
function helper() {
    return 1;
}

helper();
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
	if functions, ok := callerPayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			if name, _ := function["name"].(string); name == "helper" {
				function["uid"] = "content-entity:php-helper"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-php",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-php",
				"relative_path":    "top.php",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0; rows=%#v", len(rows), rows)
	}
}
