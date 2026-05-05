package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesKotlinPrimaryConstructorCallsToClassEntities(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Classes.kt")
	if err := os.WriteFile(callerPath, []byte(`package comprehensive

class Person(val name: String, val age: Int) {
    companion object {
        fun create(name: String): Person = Person(name, 0)
    }

    fun greet(): String = "Hi, I'm $name"
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
	if classes, ok := callerPayload["classes"].([]map[string]any); ok {
		for _, classItem := range classes {
			name, _ := classItem["name"].(string)
			if name == "Person" {
				classItem["uid"] = "content-entity:kotlin-person-class"
			}
		}
	}
	if functions, ok := callerPayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			name, _ := function["name"].(string)
			classContext, _ := function["class_context"].(string)
			switch {
			case name == "create" && classContext == "Person":
				function["uid"] = "content-entity:kotlin-person-create"
			case name == "greet" && classContext == "Person":
				function["uid"] = "content-entity:kotlin-person-greet"
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
				"relative_path":    "Classes.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	for _, row := range rows {
		if got, want := row["callee_entity_id"], "content-entity:kotlin-person-class"; got == want {
			return
		}
	}
	t.Fatalf("constructor call rows = %#v, want callee_entity_id content-entity:kotlin-person-class", rows)
}
