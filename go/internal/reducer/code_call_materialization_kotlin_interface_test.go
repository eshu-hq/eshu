package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesKotlinInterfaceTypedReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Service.kt")
	if err := os.WriteFile(callerPath, []byte(`package comprehensive

interface IService {
    fun execute(): String = "ok"
}

class Service : IService {
    override fun execute(): String = "ok"
}

fun createService(): IService = Service()

fun usage(): String {
    val service = createService()
    return service.execute()
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
			case name == "usage":
				function["end_line"] = 15
				function["uid"] = "content-entity:kotlin-usage"
			case name == "execute" && classContext == "IService":
				function["uid"] = "content-entity:kotlin-interface-execute"
			case name == "createService":
				function["uid"] = "content-entity:kotlin-create-service"
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
				"relative_path":    "Service.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	// The bare `createService()` call and the interface-typed `service.execute()`
	// receiver call both resolve.
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	assertKotlinReducerRowResolves(t, rows, "content-entity:kotlin-create-service", "createService")
	assertKotlinReducerRowResolves(t, rows, "content-entity:kotlin-interface-execute", "service.execute")
}

// assertKotlinReducerRowResolves asserts a resolved call row with the given
// callee entity id and full_name is present, independent of row order.
func assertKotlinReducerRowResolves(t *testing.T, rows []map[string]any, calleeEntityID string, fullName string) {
	t.Helper()
	for _, row := range rows {
		if row["callee_entity_id"] == calleeEntityID && row["full_name"] == fullName {
			return
		}
	}
	t.Fatalf("rows=%#v, want callee_entity_id=%q full_name=%q", rows, calleeEntityID, fullName)
}
