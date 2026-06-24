// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsEmitsInstantiatesEdge(t *testing.T) {
	t.Parallel()

	appPath := "/repo/app.py"
	widgetPath := "/repo/widget.py"
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-inst",
			"relative_path": "app.py",
			"parsed_file_data": map[string]any{
				"path": appPath,
				"functions": []any{
					map[string]any{"name": "build", "line_number": 1, "end_line": 5, "uid": "uid:build"},
				},
				"function_calls": []any{
					map[string]any{"name": "Widget", "full_name": "Widget", "call_kind": "constructor_call", "line_number": 2, "lang": "python"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-inst",
			"relative_path": "widget.py",
			"parsed_file_data": map[string]any{
				"path": widgetPath,
				"classes": []any{
					map[string]any{"name": "Widget", "line_number": 1, "end_line": 4, "uid": "uid:widget-class"},
				},
				"functions": []any{
					map[string]any{"name": "__init__", "class_context": "Widget", "line_number": 2, "end_line": 3, "uid": "uid:widget-init"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)

	var instantiates map[string]any
	for _, row := range rows {
		if row["relationship_type"] == "INSTANTIATES" {
			instantiates = row
		}
	}
	if instantiates == nil {
		t.Fatalf("no INSTANTIATES row emitted; rows=%#v", rows)
	}
	if got, want := instantiates["caller_entity_id"], "uid:build"; got != want {
		t.Errorf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := instantiates["callee_entity_id"], "uid:widget-class"; got != want {
		t.Errorf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := instantiates["callee_entity_type"], "Class"; got != want {
		t.Errorf("callee_entity_type = %#v, want %#v", got, want)
	}
	if got, want := instantiates["resolution_method"], codeprovenance.MethodTypeInferred; got != want {
		t.Errorf("resolution_method = %#v, want %#v", got, want)
	}
}

// TestExtractCodeCallRowsInstantiatesRequiresConstructorCall proves a normal
// function call does not produce an INSTANTIATES edge.
func TestExtractCodeCallRowsInstantiatesRequiresConstructorCall(t *testing.T) {
	t.Parallel()

	path := "/repo/mod.py"
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-noinst",
			"relative_path": "mod.py",
			"parsed_file_data": map[string]any{
				"path": path,
				"functions": []any{
					map[string]any{"name": "caller", "line_number": 1, "end_line": 4, "uid": "uid:caller"},
					map[string]any{"name": "helper", "line_number": 6, "end_line": 7, "uid": "uid:helper"},
				},
				"function_calls": []any{
					map[string]any{"name": "helper", "full_name": "helper", "line_number": 2, "lang": "python"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	for _, row := range rows {
		if row["relationship_type"] == "INSTANTIATES" {
			t.Fatalf("unexpected INSTANTIATES row for non-constructor call: %#v", row)
		}
	}
}
