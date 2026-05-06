package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesKotlinSafeCallReceiverChainsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Service {
    fun info(): String = "ok"
}

class Session {
    val service: Service = Service()
}

fun usage(): String {
    val session = Session()
    return session?.service?.info()
}
`)

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
				function["end_line"] = 13
				function["uid"] = "content-entity:kotlin-usage"
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
				"relative_path":    "Usage.kt",
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
	if got, want := rows[0]["full_name"], "session.service.info"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinSafeCallAliasChainsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Service {
    fun info(): String = "ok"
}

class Session {
    val service: Service = Service()
}

fun usage(): String {
    val session = Session()
    val provider = session?.service
    return provider.info()
}
`)

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
				function["end_line"] = 14
				function["uid"] = "content-entity:kotlin-usage"
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
				"relative_path":    "Usage.kt",
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
	if got, want := rows[0]["full_name"], "provider.info"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinConstructorRootReceiverChainsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun usage(): String {
    return Factory().createService().info()
}
`)

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
				function["end_line"] = 13
				function["uid"] = "content-entity:kotlin-usage"
			case name == "createService" && classContext == "Factory":
				function["uid"] = "content-entity:kotlin-factory-create-service"
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
				"relative_path":    "Usage.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	want := map[string]string{
		"content-entity:kotlin-factory-create-service": "Factory().createService",
		"content-entity:kotlin-service-info":           "Factory().createService().info",
	}
	for _, row := range rows {
		calleeID, _ := row["callee_entity_id"].(string)
		fullName, _ := row["full_name"].(string)
		if expectedFullName, ok := want[calleeID]; ok {
			if fullName != expectedFullName {
				t.Fatalf("full_name = %#v, want %#v for callee %#v", fullName, expectedFullName, calleeID)
			}
			delete(want, calleeID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expected callee rows: %#v; rows=%#v", want, rows)
	}
}
