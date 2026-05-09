package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsAnnotatesFunctionEndpointTypes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "handlers", "accounts.ts")
	calleePath := filepath.Join(repoRoot, "lib", "audit.ts")
	writeReducerTestFile(t, callerPath, `import { recordAudit } from "../lib/audit";

export function createAccount() {
  return recordAudit();
}
`)
	writeReducerTestFile(t, calleePath, `export function recordAudit() {
  return "ok";
}
`)

	rows := parsedJavaScriptCodeCallRows(t, repoRoot, callerPath, "createAccount", calleePath, "recordAudit")
	for _, row := range rows {
		if row["caller_entity_id"] == "content-entity:createAccount" && row["callee_entity_id"] == "content-entity:recordAudit" {
			if got, want := row["caller_entity_type"], "Function"; got != want {
				t.Fatalf("caller_entity_type = %#v, want %#v", got, want)
			}
			if got, want := row["callee_entity_type"], "Function"; got != want {
				t.Fatalf("callee_entity_type = %#v, want %#v", got, want)
			}
			return
		}
	}
	t.Fatalf("rows=%#v, want createAccount -> recordAudit row", rows)
}

func TestExtractCodeCallRowsAnnotatesFileRootEndpointType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	scriptPath := filepath.Join(repoRoot, "scripts", "seed.ts")
	writeReducerTestFile(t, scriptPath, `function seed() {
  return "ready";
}

seed();
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{scriptPath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, scriptPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(script) error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, payload, "seed", "content-entity:seed")
	relativePath := reducerTestRelativePath(t, repoRoot, scriptPath)

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-ts",
				"imports_map": importsMap,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-ts",
				"relative_path":    relativePath,
				"parsed_file_data": payload,
			},
		},
	})

	for _, row := range rows {
		if row["caller_entity_id"] == "repo-ts:"+relativePath && row["callee_entity_id"] == "content-entity:seed" {
			if got, want := row["caller_entity_type"], "File"; got != want {
				t.Fatalf("caller_entity_type = %#v, want %#v", got, want)
			}
			if got, want := row["callee_entity_type"], "Function"; got != want {
				t.Fatalf("callee_entity_type = %#v, want %#v", got, want)
			}
			return
		}
	}
	t.Fatalf("rows=%#v, want file root -> seed row", rows)
}
