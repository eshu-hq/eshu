package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesParsedCommonJSNamespaceRequire(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "handlers", "remoteid.js")
	calleePath := filepath.Join(repoRoot, "server", "resources", "jwt.js")
	writeReducerTestFile(t, callerPath, `const jwt = require('../resources/jwt');

module.exports.post = async req => {
  return jwt.encode(req.payload);
};
`)
	writeReducerTestFile(t, calleePath, `module.exports.encode = async data => {
  return String(data);
};
`)

	rows := parsedJavaScriptCodeCallRows(t, repoRoot, callerPath, "post", calleePath, "encode")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v", len(rows), rows)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:encode"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
	}
}

func TestExtractCodeCallRowsResolvesParsedCommonJSDestructuredRequireAlias(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "handlers", "remoteid.js")
	calleePath := filepath.Join(repoRoot, "server", "resources", "jwt.js")
	writeReducerTestFile(t, callerPath, `const { encode: sign } = require('../resources/jwt');

module.exports.post = async req => {
  return sign(req.payload);
};
`)
	writeReducerTestFile(t, calleePath, `const encode = async data => {
  return String(data);
};

module.exports = { encode };
`)

	rows := parsedJavaScriptCodeCallRows(t, repoRoot, callerPath, "post", calleePath, "encode")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v", len(rows), rows)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:encode"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
	}
}

func parsedJavaScriptCodeCallRows(
	t *testing.T,
	repoRoot string,
	callerPath string,
	callerName string,
	calleePath string,
	calleeName string,
) []map[string]any {
	t.Helper()

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
	assignReducerTestFunctionUID(t, callerPayload, callerName, "content-entity:"+callerName)
	assignReducerTestFunctionUID(t, calleePayload, calleeName, "content-entity:"+calleeName)

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
				"relative_path":    reducerTestRelativePath(t, repoRoot, calleePath),
				"parsed_file_data": calleePayload,
			},
		},
	})
	return rows
}
