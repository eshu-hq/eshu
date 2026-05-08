package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesParsedCommonJSNamespaceRequireObjectExport(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "controller", "searchController.js")
	calleePath := filepath.Join(repoRoot, "model", "searchModel.js")
	writeReducerTestFile(t, callerPath, `const searchModel = require('../model/searchModel');

const getKeys = async (request) => {
  return searchModel.getKeys(request.server.pool);
};

module.exports = { getKeys };
`)
	writeReducerTestFile(t, calleePath, `const getKeys = async (pool) => {
  return pool.query('select 1');
};

module.exports = { getKeys };
`)

	callerPayload, importsMap, rows := parsedJavaScriptCodeCallRowsWithDistinctUIDs(
		t,
		repoRoot,
		callerPath,
		calleePath,
		map[string]string{"getKeys": "content-entity:caller-getKeys"},
		map[string]string{"getKeys": "content-entity:callee-getKeys"},
	)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; caller_imports=%#v; caller_calls=%#v; imports_map=%#v", len(rows), rows, callerPayload["imports"], callerPayload["function_calls"], importsMap)
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:caller-getKeys"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:callee-getKeys"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
	}
}

func TestExtractCodeCallRowsResolvesParsedCommonJSNamespaceRequireObjectExportsWithMatchingControllerNames(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "controller", "accountController.js")
	calleePath := filepath.Join(repoRoot, "model", "accountModel.js")
	writeReducerTestFile(t, callerPath, `const accountModel = require('../model/accountModel');

const list = async (request) => {
  return accountModel.list(request.server.pool);
};

const update = async (request) => {
  return accountModel.update(request.server.pool, request.payload);
};

module.exports = { list, update };
`)
	writeReducerTestFile(t, calleePath, `const list = async (pool) => {
  return pool.query('select 1');
};

const update = async (pool, payload) => {
  return pool.query('update table set value = ?', [payload.value]);
};

module.exports = { list, update };
`)

	callerPayload, importsMap, rows := parsedJavaScriptCodeCallRowsWithDistinctUIDs(
		t,
		repoRoot,
		callerPath,
		calleePath,
		map[string]string{
			"list":   "content-entity:caller-list",
			"update": "content-entity:caller-update",
		},
		map[string]string{
			"list":   "content-entity:callee-list",
			"update": "content-entity:callee-update",
		},
	)

	assertReducerCodeCallRow(t, rows, "content-entity:caller-list", "content-entity:callee-list")
	assertReducerCodeCallRow(t, rows, "content-entity:caller-update", "content-entity:callee-update")
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v; caller_imports=%#v; caller_calls=%#v; imports_map=%#v", len(rows), rows, callerPayload["imports"], callerPayload["function_calls"], importsMap)
	}
}

func TestExtractCodeCallRowsPrefersCommonJSImportOverSameFileNestedTrailingName(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "controller", "accountController.js")
	calleePath := filepath.Join(repoRoot, "model", "accountModel.js")
	writeReducerTestFile(t, callerPath, `const accountModel = require('../model/accountModel');

const outer = async (request) => {
  const list = () => [];
  return accountModel.list(request.server.pool);
};

module.exports = { outer };
`)
	writeReducerTestFile(t, calleePath, `const list = async (pool) => {
  return pool.query('select 1');
};

module.exports = { list };
`)

	callerPayload, _, rows := parsedJavaScriptCodeCallRowsWithDistinctUIDs(
		t,
		repoRoot,
		callerPath,
		calleePath,
		map[string]string{
			"outer": "content-entity:outer",
			"list":  "content-entity:local-list",
		},
		map[string]string{"list": "content-entity:model-list"},
	)

	assertReducerCodeCallRow(t, rows, "content-entity:outer", "content-entity:model-list")
	for _, row := range rows {
		if row["callee_entity_id"] == "content-entity:local-list" {
			t.Fatalf("resolved qualified imported call to same-file nested function; rows=%#v; caller_calls=%#v", rows, callerPayload["function_calls"])
		}
	}
}

func parsedJavaScriptCodeCallRowsWithDistinctUIDs(
	t *testing.T,
	repoRoot string,
	callerPath string,
	calleePath string,
	callerUIDs map[string]string,
	calleeUIDs map[string]string,
) (map[string]any, map[string][]string, []map[string]any) {
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
	for name, uid := range callerUIDs {
		assignReducerTestFunctionUID(t, callerPayload, name, uid)
	}
	for name, uid := range calleeUIDs {
		assignReducerTestFunctionUID(t, calleePayload, name, uid)
	}

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
	return callerPayload, importsMap, rows
}
