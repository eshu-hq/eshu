package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesKotlinIfSmartCastReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinSmartCastFile(
		t,
		callerPath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun usage(any: Any): String {
    if (any is Service) {
        return any.info()
    }
    return ""
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	stampKotlinSmartCastFunctionUIDs(callerPayload, map[string]string{
		"usage":        "content-entity:kotlin-usage",
		"Service.info": "content-entity:kotlin-service-info",
	})

	_, rows := ExtractCodeCallRows([]facts.Envelope{
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
	})

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-service-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinWhenSmartCastReceiverChainsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinSmartCastFile(
		t,
		callerPath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun usage(value: Any): String {
    return when (value) {
        is Factory -> value.createService().info()
        else -> ""
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	stampKotlinSmartCastFunctionUIDs(callerPayload, map[string]string{
		"usage":                 "content-entity:kotlin-usage",
		"Factory.createService": "content-entity:kotlin-factory-create-service",
		"Service.info":          "content-entity:kotlin-service-info",
	})

	_, rows := ExtractCodeCallRows([]facts.Envelope{
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
	})

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	want := map[string]string{
		"value.createService":        "content-entity:kotlin-factory-create-service",
		"value.createService().info": "content-entity:kotlin-service-info",
	}
	for _, row := range rows {
		fullName, _ := row["full_name"].(string)
		if wantUID, ok := want[fullName]; ok {
			if got := row["callee_entity_id"]; got != wantUID {
				t.Fatalf("callee_entity_id(%q) = %#v, want %#v", fullName, got, wantUID)
			}
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing rows for %#v in %#v", want, rows)
	}
}

func TestExtractCodeCallRowsResolvesKotlinGenericSmartCastReceiverChainsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinSmartCastFile(
		t,
		callerPath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class ServiceBox<T>(private val value: T) {
    fun boxed(): T = value
}

fun usage(receiver: Any): String {
    if (receiver is ServiceBox<Service>) {
        return receiver.boxed().info()
    }
    return ""
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	stampKotlinSmartCastFunctionUIDs(callerPayload, map[string]string{
		"usage":            "content-entity:kotlin-usage",
		"ServiceBox.boxed": "content-entity:kotlin-service-box-boxed",
		"Service.info":     "content-entity:kotlin-service-info",
	})

	_, rows := ExtractCodeCallRows([]facts.Envelope{
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
	})

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	want := map[string]string{
		"receiver.boxed":        "content-entity:kotlin-service-box-boxed",
		"receiver.boxed().info": "content-entity:kotlin-service-info",
	}
	for _, row := range rows {
		fullName, _ := row["full_name"].(string)
		if wantUID, ok := want[fullName]; ok {
			if got := row["callee_entity_id"]; got != wantUID {
				t.Fatalf("callee_entity_id(%q) = %#v, want %#v", fullName, got, wantUID)
			}
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing rows for %#v in %#v", want, rows)
	}
}

func writeKotlinSmartCastFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}

func stampKotlinSmartCastFunctionUIDs(payload map[string]any, functionUIDs map[string]string) {
	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		return
	}
	for _, function := range functions {
		name, _ := function["name"].(string)
		classContext, _ := function["class_context"].(string)
		key := name
		if classContext != "" {
			key = classContext + "." + name
		}
		if uid, ok := functionUIDs[key]; ok {
			function["uid"] = uid
		}
		if name == "usage" {
			function["end_line"] = 999
		}
	}
}
