package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesJavaScriptRegistryDestructuredAlias(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "orders.js")
	writeReducerTestFile(t, filePath, `function createOrder(order) {
  return order;
}

module.exports.run = function run(order) {
  const handlers = {
    create: createOrder,
  };
  const { create } = handlers;
  return create(order);
};
`)

	rows := parsedDynamicJavaScriptCodeCallRows(t, repoRoot, filePath, map[string]string{
		"createOrder": "content-entity:create-order",
		"run":         "content-entity:run",
	})

	assertReducerCodeCallRow(t, rows, "content-entity:run", "content-entity:create-order")
}

func TestExtractCodeCallRowsResolvesJavaScriptRegistryApplyReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "orders.js")
	writeReducerTestFile(t, filePath, `function archiveOrder(order) {
  return order;
}

module.exports.run = function run(order) {
  const handlers = {
    archive: archiveOrder,
  };
  return handlers.archive.apply(null, [order]);
};
`)

	rows := parsedDynamicJavaScriptCodeCallRows(t, repoRoot, filePath, map[string]string{
		"archiveOrder": "content-entity:archive-order",
		"run":          "content-entity:run",
	})

	assertReducerCodeCallRow(t, rows, "content-entity:run", "content-entity:archive-order")
}

func TestExtractCodeCallRowsResolvesJavaScriptStaticModuleExportKey(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "orders.js")
	writeReducerTestFile(t, filePath, `function refreshOrder(order) {
  return order;
}

module.exports["refreshOrder"] = refreshOrder;

module.exports.run = function run(order) {
  const action = "refreshOrder";
  return module.exports[action].call(null, order);
};
`)

	rows := parsedDynamicJavaScriptCodeCallRows(t, repoRoot, filePath, map[string]string{
		"refreshOrder": "content-entity:refresh-order",
		"run":          "content-entity:run",
	})

	assertReducerCodeCallRow(t, rows, "content-entity:run", "content-entity:refresh-order")
}

func parsedDynamicJavaScriptCodeCallRows(
	t *testing.T,
	repoRoot string,
	filePath string,
	functionUIDs map[string]string,
) []map[string]any {
	t.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{filePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	for name, uid := range functionUIDs {
		assignReducerTestFunctionUID(t, payload, name, uid)
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
				"relative_path":    reducerTestRelativePath(t, repoRoot, filePath),
				"parsed_file_data": payload,
			},
		},
	})
	return rows
}
