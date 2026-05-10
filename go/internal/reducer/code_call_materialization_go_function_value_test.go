package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesGoCrossFileFunctionValueReference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "main.go")
	if err := os.WriteFile(callerPath, []byte(`package main

type CLI struct {
	HelpFunc func()
}

func run() {
	cli := CLI{HelpFunc: helpFunc}
	_ = cli
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}
	calleePath := filepath.Join(repoRoot, "help.go")
	if err := os.WriteFile(calleePath, []byte(`package main

func helpFunc() {}
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
	assignFunctionUID(t, callerPayload, "run", "", "content-entity:run")
	assignFunctionUID(t, calleePayload, "helpFunc", "", "content-entity:help-func")

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
				"relative_path":    "main.go",
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "help.go",
				"parsed_file_data": calleePayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRowWithKind(t, rows, "content-entity:run", "content-entity:help-func", "go.function_value_reference")
}

func assignFunctionUID(t *testing.T, payload map[string]any, name string, classContext string, uid string) {
	t.Helper()
	for _, function := range payload["functions"].([]map[string]any) {
		functionName, _ := function["name"].(string)
		functionClassContext, _ := function["class_context"].(string)
		if functionName == name && functionClassContext == classContext {
			function["uid"] = uid
			return
		}
	}
	t.Fatalf("missing function %s/%s in %#v", classContext, name, payload["functions"])
}

func assertCodeCallRowWithKind(
	t *testing.T,
	rows []map[string]any,
	callerID string,
	calleeID string,
	callKind string,
) {
	t.Helper()
	for _, row := range rows {
		if anyToString(row["caller_entity_id"]) == callerID &&
			anyToString(row["callee_entity_id"]) == calleeID &&
			anyToString(row["call_kind"]) == callKind {
			if got := anyToString(row["relationship_type"]); got != "REFERENCES" {
				t.Fatalf("relationship_type = %q, want REFERENCES in %#v", got, row)
			}
			return
		}
	}
	t.Fatalf("missing code-call row %s -> %s (%s) in %#v", callerID, calleeID, callKind, rows)
}
