package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
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
