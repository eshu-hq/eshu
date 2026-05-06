package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesKotlinPrimaryConstructorPropertyReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Worker.kt")
	if err := os.WriteFile(callerPath, []byte(`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Worker(private val service: Service) {
    fun run(): String {
        return service.info()
    }
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
			case name == "run" && classContext == "Worker":
				function["end_line"] = 9
				function["uid"] = "content-entity:kotlin-worker-run"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
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
				"relative_path":    "Worker.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-service-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "service.info"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}
