package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesGoReceiverVariableCallsWithoutTreatingImportsAsLocal(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "handler.go")
	if err := os.WriteFile(callerPath, []byte(`package query

import "fmt"

type CodeHandler struct{}

func Println() {}

func (h *CodeHandler) transitiveRelationshipsGraphRow() {}

func (h *CodeHandler) handleRelationships() {
	h.transitiveRelationshipsGraphRow()
	fmt.Println("hello")
}
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
			case name == "handleRelationships":
				function["end_line"] = 12
				function["uid"] = "content-entity:go-handle-relationships"
			case name == "transitiveRelationshipsGraphRow" && classContext == "CodeHandler":
				function["uid"] = "content-entity:go-transitive-relationships-row"
			case name == "Println":
				function["uid"] = "content-entity:go-local-println"
			}
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
				"relative_path":    "handler.go",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:go-handle-relationships"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:go-transitive-relationships-row"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "h.transitiveRelationshipsGraphRow"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}
