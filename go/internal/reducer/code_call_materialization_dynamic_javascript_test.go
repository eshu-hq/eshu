package reducer

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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

func BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls(b *testing.B) {
	envelopes := largeJavaScriptDynamicCallEnvelopes(500)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, rows := ExtractCodeCallRows(envelopes)
		if len(rows) != 500 {
			b.Fatalf("len(rows) = %d, want 500", len(rows))
		}
	}
}

func BenchmarkResolveDynamicJavaScriptCalleeAnonymousFunctionSource(b *testing.B) {
	envelopes := largeJavaScriptDynamicCallEnvelopes(500)
	fileData := envelopes[1].Payload["parsed_file_data"].(map[string]any)
	functions := fileData["functions"].([]any)
	functions[1].(map[string]any)["uid"] = ""
	call := fileData["function_calls"].([]any)[0].(map[string]any)
	index := buildCodeEntityIndex(envelopes)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entityID := resolveDynamicJavaScriptCalleeEntityID(
			index,
			"bundle.js",
			"bundle.js",
			fileData,
			call,
		)
		if entityID != "content-entity:target-func" {
			b.Fatalf("entityID = %q, want content-entity:target-func", entityID)
		}
	}
}

func BenchmarkResolveDynamicJavaScriptCalleeNoAliasFunctionSource(b *testing.B) {
	envelopes := largeJavaScriptNoAliasCallEnvelopes(500)
	fileData := envelopes[1].Payload["parsed_file_data"].(map[string]any)
	call := fileData["function_calls"].([]any)[0].(map[string]any)
	index := buildCodeEntityIndex(envelopes)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entityID := resolveDynamicJavaScriptCalleeEntityID(
			index,
			"bundle.js",
			"bundle.js",
			fileData,
			call,
		)
		if entityID != "" {
			b.Fatalf("entityID = %q, want empty", entityID)
		}
	}
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

func largeJavaScriptDynamicCallEnvelopes(callCount int) []facts.Envelope {
	source := strings.Builder{}
	source.WriteString("function targetFunc() { return true; }\n")
	source.WriteString("function run() {\n")
	source.WriteString("  const handlers = {\n")
	source.WriteString("    create: targetFunc,\n")
	for i := 0; i < 1500; i++ {
		source.WriteString("    extra")
		source.WriteString(strconv.Itoa(i))
		source.WriteString(": targetFunc,\n")
	}
	source.WriteString("  };\n")
	for i := 0; i < callCount; i++ {
		source.WriteString("  handlers.create.call(null);\n")
	}
	source.WriteString("}\n")

	functions := []any{
		map[string]any{
			"uid":         "content-entity:target-func",
			"name":        "targetFunc",
			"line_number": 1,
			"end_line":    1,
		},
		map[string]any{
			"uid":         "content-entity:run",
			"name":        "run",
			"line_number": 2,
			"end_line":    callCount + 1506,
			"source":      source.String(),
		},
	}
	calls := make([]any, 0, callCount)
	for i := 0; i < callCount; i++ {
		calls = append(calls, map[string]any{
			"lang":        "javascript",
			"name":        "call",
			"full_name":   "handlers.create.call",
			"line_number": i + 1506,
		})
	}

	return []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-js",
				"imports_map": map[string]any{},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-js",
				"relative_path": "bundle.js",
				"parsed_file_data": map[string]any{
					"path":           "bundle.js",
					"functions":      functions,
					"function_calls": calls,
				},
			},
			ObservedAt: time.Unix(0, 0).UTC(),
		},
	}
}

func largeJavaScriptNoAliasCallEnvelopes(callCount int) []facts.Envelope {
	source := strings.Builder{}
	source.WriteString("function run() {\n")
	for i := 0; i < 1500; i++ {
		source.WriteString("  external")
		source.WriteString(strconv.Itoa(i))
		source.WriteString("();\n")
	}
	source.WriteString("}\n")

	calls := make([]any, 0, callCount)
	for i := 0; i < callCount; i++ {
		calls = append(calls, map[string]any{
			"lang":        "javascript",
			"name":        "missing",
			"full_name":   "missing.call",
			"line_number": 2,
		})
	}

	return []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-js",
				"imports_map": map[string]any{},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-js",
				"relative_path": "bundle.js",
				"parsed_file_data": map[string]any{
					"path": "bundle.js",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 1,
							"end_line":    1502,
							"source":      source.String(),
						},
					},
					"function_calls": calls,
				},
			},
			ObservedAt: time.Unix(0, 0).UTC(),
		},
	}
}
