package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesTypeScriptBaseURLImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "handlers", "token.ts")
	calleePath := filepath.Join(repoRoot, "server", "resources", "jwt.ts")
	writeReducerTestFile(t, filepath.Join(repoRoot, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": "."
  }
}
`)
	writeReducerTestFile(t, callerPath, `import * as jwt from "server/resources/jwt";
import { encode as directEncode } from "server/resources/jwt";

export const post = async req => {
  const first = await jwt.encode(req.payload);
  const second = await directEncode(req.payload);
  return { first, second };
};
`)
	writeReducerTestFile(t, calleePath, `export const encode = async data => {
  return String(data);
};
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{callerPath, calleePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(caller) error = %v, want nil", err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(callee) error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, callerPayload, "post", "content-entity:ts-post")
	assignReducerTestFunctionUID(t, calleePayload, "encode", "content-entity:ts-encode")

	envelopes := []facts.Envelope{
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
				"relative_path":    reducerTestRelativePath(t, repoRoot, callerPath),
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-ts",
				"relative_path":    reducerTestRelativePath(t, repoRoot, calleePath),
				"parsed_file_data": calleePayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d; rows=%#v; caller_imports=%#v; caller_calls=%#v; imports_map=%#v", got, want, rows, callerPayload["imports"], callerPayload["function_calls"], importsMap)
	}
	for _, row := range rows {
		if got, want := row["callee_entity_id"], "content-entity:ts-encode"; got != want {
			t.Fatalf("callee_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
		}
		if got, want := row["callee_file"], "server/resources/jwt.ts"; got != want {
			t.Fatalf("callee_file = %#v, want %#v; rows=%#v", got, want, rows)
		}
	}
}
