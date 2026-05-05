package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesKotlinSameFileFunctionReturnTypeAliasCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")
	if err := os.WriteFile(callerPath, []byte(`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service = Service()

fun usage(): String {
    val provider = createService()
    return provider.info()
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
				function["end_line"] = 11
				function["uid"] = "content-entity:kotlin-usage"
			case name == "createService" && classContext == "":
				function["uid"] = "content-entity:kotlin-create-service"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}
	if classes, ok := callerPayload["classes"].([]map[string]any); ok {
		for _, classItem := range classes {
			name, _ := classItem["name"].(string)
			if name == "Service" {
				classItem["uid"] = "content-entity:kotlin-service-class"
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

	for _, row := range rows {
		if got, want := row["callee_entity_id"], "content-entity:kotlin-service-info"; got == want {
			if gotName, wantName := row["full_name"], "provider.info"; gotName != wantName {
				t.Fatalf("full_name = %#v, want %#v", gotName, wantName)
			}
			return
		}
	}
	t.Fatalf("rows=%#v, want callee_entity_id content-entity:kotlin-service-info", rows)
}

func TestExtractCodeCallRowsResolvesKotlinSameFileFunctionReturnAliasChainCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")
	if err := os.WriteFile(callerPath, []byte(`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service = Service()

fun usage(): String {
    val provider = createService()
    val active = provider
    return active.info()
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
				function["end_line"] = 12
				function["uid"] = "content-entity:kotlin-usage"
			case name == "createService" && classContext == "":
				function["uid"] = "content-entity:kotlin-create-service"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}
	if classes, ok := callerPayload["classes"].([]map[string]any); ok {
		for _, classItem := range classes {
			name, _ := classItem["name"].(string)
			if name == "Service" {
				classItem["uid"] = "content-entity:kotlin-service-class"
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

	for _, row := range rows {
		if got, want := row["callee_entity_id"], "content-entity:kotlin-service-info"; got == want {
			if gotName, wantName := row["full_name"], "active.info"; gotName != wantName {
				t.Fatalf("full_name = %#v, want %#v", gotName, wantName)
			}
			return
		}
	}
	t.Fatalf("rows=%#v, want callee_entity_id content-entity:kotlin-service-info", rows)
}
