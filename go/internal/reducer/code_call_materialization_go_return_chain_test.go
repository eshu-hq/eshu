// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/goldenaudit"
)

func TestExtractCodeCallRowsResolvesGoMethodReturnReceiverChain(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "internal/terraform/node.go",
				"parsed_file_data": map[string]any{
					"path": "internal/terraform/node.go",
					"functions": []any{
						map[string]any{"name": "Execute", "line_number": 20, "end_line": 70, "uid": "content-entity:execute"},
					},
					"function_calls": []any{
						map[string]any{
							"name":                      "GetActionInstance",
							"full_name":                 "ctx.Actions().GetActionInstance",
							"line_number":               40,
							"lang":                      "go",
							"chain_receiver_identifier": "ctx",
							"chain_receiver_method":     "Actions",
							"chain_receiver_obj_type":   "BuiltinEvalContext",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "internal/terraform/eval_context_builtin.go",
				"parsed_file_data": map[string]any{
					"path": "internal/terraform/eval_context_builtin.go",
					"functions": []any{
						map[string]any{
							"name":          "Actions",
							"class_context": "BuiltinEvalContext",
							"return_type":   "Actions",
							"line_number":   665,
							"end_line":      667,
							"uid":           "content-entity:builtin-actions",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "internal/actions/actions.go",
				"parsed_file_data": map[string]any{
					"path": "internal/actions/actions.go",
					"functions": []any{
						map[string]any{
							"name":          "GetActionInstance",
							"class_context": "Actions",
							"line_number":   49,
							"end_line":      60,
							"uid":           "content-entity:get-action-instance",
						},
					},
				},
			},
		},
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-other",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-other",
				"relative_path": "internal/terraform/eval_context_builtin.go",
				"parsed_file_data": map[string]any{
					"path": "internal/terraform/eval_context_builtin.go",
					"functions": []any{
						map[string]any{
							"name":          "Actions",
							"class_context": "BuiltinEvalContext",
							"return_type":   "Widget",
							"line_number":   665,
							"end_line":      667,
							"uid":           "content-entity:other-actions",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "content-entity:execute", "content-entity:get-action-instance")
}

func TestExtractCodeCallRowsResolvesGoInterfaceConcreteAssignmentReturnChain(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "internal", "terraform", "node.go")
	contextPath := filepath.Join(repoRoot, "internal", "terraform", "eval_context_builtin.go")
	actionsPath := filepath.Join(repoRoot, "internal", "actions", "actions.go")
	for _, path := range []string{callerPath, contextPath, actionsPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(callerPath, []byte(`package terraform

type EvalContext interface {
	Actions() *Actions
}

func Execute() {
	var ctx EvalContext = &BuiltinEvalContext{}
	ctx.Actions().GetActionInstance()
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}
	if err := os.WriteFile(contextPath, []byte(`package terraform

type BuiltinEvalContext struct{}
type Actions struct{}

func (ctx *BuiltinEvalContext) Actions() *Actions {
	return &Actions{}
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", contextPath, err)
	}
	if err := os.WriteFile(actionsPath, []byte(`package actions

type Actions struct{}

func (a *Actions) GetActionInstance() {}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", actionsPath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	contextPayload, err := engine.ParsePath(repoRoot, contextPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", contextPath, err)
	}
	actionsPayload, err := engine.ParsePath(repoRoot, actionsPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", actionsPath, err)
	}
	for _, function := range callerPayload["functions"].([]map[string]any) {
		if name, _ := function["name"].(string); name == "Execute" {
			function["uid"] = "content-entity:execute"
		}
	}
	for _, function := range contextPayload["functions"].([]map[string]any) {
		if name, _ := function["name"].(string); name == "Actions" {
			function["uid"] = "content-entity:builtin-actions"
		}
	}
	for _, function := range actionsPayload["functions"].([]map[string]any) {
		if name, _ := function["name"].(string); name == "GetActionInstance" {
			function["uid"] = "content-entity:get-action-instance"
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "internal/terraform/node.go",
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "internal/terraform/eval_context_builtin.go",
				"parsed_file_data": contextPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "internal/actions/actions.go",
				"parsed_file_data": actionsPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "content-entity:execute", "content-entity:get-action-instance")
	assertGoInterfaceReceiverChainGolden(t, rows)
}

func TestExtractCodeCallRowsSkipsGoMethodReturnChainWithoutReceiverTypeProof(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "internal/terraform/node.go",
				"parsed_file_data": map[string]any{
					"path": "internal/terraform/node.go",
					"functions": []any{
						map[string]any{"name": "Execute", "line_number": 20, "end_line": 70, "uid": "content-entity:execute"},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "GetActionInstance",
							"full_name":   "external.Actions().GetActionInstance",
							"line_number": 40,
							"lang":        "go",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "internal/terraform/eval_context_builtin.go",
				"parsed_file_data": map[string]any{
					"path": "internal/terraform/eval_context_builtin.go",
					"functions": []any{
						map[string]any{
							"name":          "Actions",
							"class_context": "BuiltinEvalContext",
							"return_type":   "Actions",
							"line_number":   665,
							"end_line":      667,
							"uid":           "content-entity:builtin-actions",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "internal/actions/actions.go",
				"parsed_file_data": map[string]any{
					"path": "internal/actions/actions.go",
					"functions": []any{
						map[string]any{
							"name":          "GetActionInstance",
							"class_context": "Actions",
							"line_number":   49,
							"end_line":      60,
							"uid":           "content-entity:get-action-instance",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want no unproven method-return chain edge", rows)
	}
}

func TestExtractCodeCallRowsResolvesGoPackageQualifiedConstructor(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "internal", "terraform", "context_walk.go")
	calleePath := filepath.Join(repoRoot, "internal", "actions", "actions.go")
	if err := os.MkdirAll(filepath.Dir(callerPath), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(callerPath), err)
	}
	if err := os.MkdirAll(filepath.Dir(calleePath), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(calleePath), err)
	}
	if err := os.WriteFile(callerPath, []byte(`package terraform

import acts "github.com/hashicorp/terraform/internal/actions"

func configureContext() {
	_ = acts.NewActions()
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}
	if err := os.WriteFile(calleePath, []byte(`package actions

type Actions struct{}

func NewActions() *Actions {
	return &Actions{}
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", calleePath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", calleePath, err)
	}
	for _, function := range callerPayload["functions"].([]map[string]any) {
		if name, _ := function["name"].(string); name == "configureContext" {
			function["uid"] = "content-entity:configure-context"
		}
	}
	for _, function := range calleePayload["functions"].([]map[string]any) {
		if name, _ := function["name"].(string); name == "NewActions" {
			function["uid"] = "content-entity:new-actions"
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "internal/terraform/context_walk.go",
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "internal/actions/actions.go",
				"parsed_file_data": calleePayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "content-entity:configure-context", "content-entity:new-actions")
}

func assertGoInterfaceReceiverChainGolden(t *testing.T, rows []map[string]any) {
	t.Helper()

	want, err := goldenaudit.LoadGoldenGraph(
		filepath.Join("..", "parser", "goldenaudit", "testdata", "go_interface_receiver_chain.json"),
	)
	if err != nil {
		t.Fatalf("LoadGoldenGraph() error = %v, want nil", err)
	}
	got := goldenaudit.Graph{
		Nodes: []goldenaudit.Node{
			{
				ID:   "content-entity:execute",
				Kind: "function",
				Name: "Execute",
				Path: "internal/terraform/node.go",
				Line: 7,
			},
			{
				ID:   "content-entity:get-action-instance",
				Kind: "function",
				Name: "GetActionInstance",
				Path: "internal/actions/actions.go",
				Line: 5,
			},
		},
		Edges: goInterfaceReceiverChainGoldenEdges(rows),
	}
	report := goldenaudit.CompareGraph(want, got)
	if !report.Pass() {
		t.Fatalf("Go interface receiver chain golden drift: %s", report.Summary())
	}
}

func goInterfaceReceiverChainGoldenEdges(rows []map[string]any) []goldenaudit.Edge {
	edges := make([]goldenaudit.Edge, 0, 1)
	for _, row := range rows {
		if anyToString(row["caller_entity_id"]) != "content-entity:execute" ||
			anyToString(row["callee_entity_id"]) != "content-entity:get-action-instance" {
			continue
		}
		relationshipType := anyToString(row["relationship_type"])
		if relationshipType == "" {
			relationshipType = "CALLS"
		}
		edges = append(edges, goldenaudit.Edge{
			SourceID: "content-entity:execute",
			TargetID: "content-entity:get-action-instance",
			Type:     relationshipType,
		})
	}
	return edges
}
