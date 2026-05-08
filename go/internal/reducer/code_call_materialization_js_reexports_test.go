package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesParsedJavaScriptStaticRelativeReExports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		barrelBody string
	}{
		{
			name:       "named re-export",
			barrelBody: `export { encode } from "./jwt";`,
		},
		{
			name:       "star re-export",
			barrelBody: `export * from "./jwt";`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			callerPath := filepath.Join(repoRoot, "server", "handlers", "remoteid.ts")
			barrelPath := filepath.Join(repoRoot, "server", "resources", "index.ts")
			calleePath := filepath.Join(repoRoot, "server", "resources", "jwt.ts")
			decoyPath := filepath.Join(repoRoot, "server", "other", "jwt.ts")
			writeReducerTestFile(t, callerPath, `import { encode } from "../resources";

export const post = async req => {
  return encode(req.payload);
};
`)
			writeReducerTestFile(t, barrelPath, tt.barrelBody)
			writeReducerTestFile(t, calleePath, `export const encode = async data => {
  return String(data);
};
`)
			writeReducerTestFile(t, decoyPath, `export const encode = async data => {
  return "wrong:" + String(data);
};
`)

			rows := parsedJavaScriptReExportCodeCallRows(t, repoRoot, callerPath, barrelPath, calleePath, decoyPath)
			if len(rows) != 1 {
				t.Fatalf("len(rows) = %d, want 1; rows=%#v", len(rows), rows)
			}
			if got, want := rows[0]["callee_entity_id"], "content-entity:encode"; got != want {
				t.Fatalf("callee_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
			}
			if got, want := rows[0]["callee_file"], "server/resources/jwt.ts"; got != want {
				t.Fatalf("callee_file = %#v, want %#v; rows=%#v", got, want, rows)
			}
		})
	}
}

func TestExtractCodeCallRowsResolvesReExportedTypeScriptConstructorsAndInstanceMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeReducerTestFile(t, filepath.Join(repoRoot, "package.json"), `{"main":"dist/service-entry.js"}`)
	callerPath := filepath.Join(repoRoot, "service-entry.ts")
	barrelPath := filepath.Join(repoRoot, "server", "files", "index.ts")
	calleePath := filepath.Join(repoRoot, "server", "files", "SnapshotSync.ts")
	writeReducerTestFile(t, callerPath, `import { SnapshotSync } from "./server/files";

const sync = new SnapshotSync();
await sync.invoke();
`)
	writeReducerTestFile(t, barrelPath, `export * from "./SnapshotSync";`)
	writeReducerTestFile(t, calleePath, `export class SnapshotSync {
  constructor() {}

  async invoke(): Promise<void> {
    await SnapshotSync.pathExists();
  }

  static async pathExists(): Promise<boolean> {
    return true;
  }
}
`)

	rows := parsedJavaScriptReExportClassCodeCallRows(t, repoRoot, callerPath, barrelPath, calleePath)
	assertReducerRowsContainCallee(t, rows, "content-entity:snapshot-sync-class")
	assertReducerRowsContainCallee(t, rows, "content-entity:snapshot-sync-constructor")
	assertReducerRowsContainCallee(t, rows, "content-entity:snapshot-sync-invoke")
	assertReducerRowsContainCallee(t, rows, "content-entity:snapshot-sync-path-exists")
}

func parsedJavaScriptReExportCodeCallRows(
	t *testing.T,
	repoRoot string,
	callerPath string,
	barrelPath string,
	calleePath string,
	decoyPath string,
) []map[string]any {
	t.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{callerPath, barrelPath, calleePath, decoyPath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(caller) error = %v, want nil", err)
	}
	barrelPayload, err := engine.ParsePath(repoRoot, barrelPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(barrel) error = %v, want nil", err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(callee) error = %v, want nil", err)
	}
	decoyPayload, err := engine.ParsePath(repoRoot, decoyPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(decoy) error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, callerPayload, "post", "content-entity:post")
	assignReducerTestFunctionUID(t, calleePayload, "encode", "content-entity:encode")
	assignReducerTestFunctionUID(t, decoyPayload, "encode", "content-entity:decoy-encode")

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-js",
				"imports_map": importsMap,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, callerPath),
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, barrelPath),
				"parsed_file_data": barrelPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, calleePath),
				"parsed_file_data": calleePayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, decoyPath),
				"parsed_file_data": decoyPayload,
			},
		},
	})
	return rows
}

func parsedJavaScriptReExportClassCodeCallRows(
	t *testing.T,
	repoRoot string,
	callerPath string,
	barrelPath string,
	calleePath string,
) []map[string]any {
	t.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{callerPath, barrelPath, calleePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(caller) error = %v, want nil", err)
	}
	barrelPayload, err := engine.ParsePath(repoRoot, barrelPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(barrel) error = %v, want nil", err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(callee) error = %v, want nil", err)
	}
	assignReducerTestClassUID(t, calleePayload, "SnapshotSync", "content-entity:snapshot-sync-class")
	assignReducerTestFunctionUID(t, calleePayload, "constructor", "content-entity:snapshot-sync-constructor")
	assignReducerTestFunctionUID(t, calleePayload, "invoke", "content-entity:snapshot-sync-invoke")
	assignReducerTestFunctionUID(t, calleePayload, "pathExists", "content-entity:snapshot-sync-path-exists")

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-js",
				"imports_map": importsMap,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, callerPath),
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, barrelPath),
				"parsed_file_data": barrelPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, calleePath),
				"parsed_file_data": calleePayload,
			},
		},
	})
	return rows
}

func assignReducerTestClassUID(t *testing.T, payload map[string]any, name string, uid string) {
	t.Helper()
	classes, ok := payload["classes"].([]map[string]any)
	if !ok {
		t.Fatalf("payload classes = %T, want []map[string]any", payload["classes"])
	}
	for i := range classes {
		if classes[i]["name"] == name {
			classes[i]["uid"] = uid
			payload["classes"] = classes
			return
		}
	}
	t.Fatalf("payload missing class %q in %#v", name, classes)
}

func assertReducerRowsContainCallee(t *testing.T, rows []map[string]any, want string) {
	t.Helper()
	for _, row := range rows {
		if row["callee_entity_id"] == want {
			return
		}
	}
	t.Fatalf("rows=%#v, want callee_entity_id %q", rows, want)
}
