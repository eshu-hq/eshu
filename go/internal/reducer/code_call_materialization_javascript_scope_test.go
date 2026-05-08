package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesJavaScriptObjectExportMethodCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "handlers", "summary.ts")
	calleePath := filepath.Join(repoRoot, "server", "resources", "summary", "index.ts")
	writeReducerTestFile(t, callerPath, `import { Summary } from "../resources/summary";

export const post = async req => {
  return Summary.summarize(req.payload);
};
`)
	writeReducerTestFile(t, calleePath, `export const Summary = {
  init(client) {
    this.client = client;
  },

  async summarize(payload) {
    return String(payload);
  },
};
`)

	rows := parsedJavaScriptCodeCallRows(t, repoRoot, callerPath, "post", calleePath, "summarize")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v", len(rows), rows)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:summarize"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
	}
}

func TestExtractCodeCallRowsResolvesDuplicateLocalHelpersInsideJavaScriptFunctionScope(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	scriptPath := filepath.Join(repoRoot, "scripts", "create-new-version.js")
	writeReducerTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "scripts": {
    "create:version": "node scripts/create-new-version.js"
  }
}
`)
	writeReducerTestFile(t, scriptPath, `const main = () => {
  updateCurrent();
  updateNext();
};

const updateCurrent = () => {
  const findAndUpdate = value => value + "-current";
  return findAndUpdate("v1");
};

const updateNext = () => {
  const findAndUpdate = value => value + "-next";
  return findAndUpdate("v2");
};

main();
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
	assignReducerTestFunctionUID(t, payload, "main", "content-entity:main")
	assignReducerTestFunctionUID(t, payload, "updateCurrent", "content-entity:update-current")
	assignReducerTestFunctionUID(t, payload, "updateNext", "content-entity:update-next")
	assignReducerTestDuplicateFunctionUIDs(t, payload, "findAndUpdate", "content-entity:current-helper", "content-entity:next-helper")

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
				"relative_path":    reducerTestRelativePath(t, repoRoot, scriptPath),
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "content-entity:update-current", "content-entity:current-helper")
	assertReducerCodeCallRow(t, rows, "content-entity:update-next", "content-entity:next-helper")
}

func TestExtractCodeCallRowsUsesFileRootForTopLevelJavaScriptCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	scriptPath := filepath.Join(repoRoot, "iac", "scripts", "prepare-parameters.js")
	writeReducerTestFile(t, scriptPath, `function createClient() {
  return "ready";
}

function helper() {
  return "helper";
}

const client = createClient();
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
	assignReducerTestFunctionUID(t, payload, "createClient", "content-entity:create-client")
	assignReducerTestFunctionUID(t, payload, "helper", "content-entity:helper")
	relativePath := reducerTestRelativePath(t, repoRoot, scriptPath)

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
				"relative_path":    relativePath,
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "repo-js:"+relativePath, "content-entity:create-client")
	assertReducerNoCodeCallRow(t, rows, "repo-js:"+relativePath, "content-entity:helper")
}

func TestExtractCodeCallRowsResolvesJavaScriptFunctionValueReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "orders.js")
	writeReducerTestFile(t, filePath, `const isEnabled = (item) => item.enabled;
module.exports.updateOrder = (order) => order;

module.exports.list = (items, async) => {
  const enabled = items.filter(isEnabled);
  async.apply(module.exports.updateOrder, enabled);
  return enabled;
};
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{filePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, payload, "isEnabled", "content-entity:is-enabled")
	assignReducerTestFunctionUID(t, payload, "updateOrder", "content-entity:update-order")
	assignReducerTestFunctionUID(t, payload, "list", "content-entity:list")

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

	assertReducerCodeCallRow(t, rows, "content-entity:list", "content-entity:is-enabled")
	assertReducerCodeCallRow(t, rows, "content-entity:list", "content-entity:update-order")
}

func TestExtractCodeCallRowsResolvesNestedRecursiveScriptHelpers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	scriptPath := filepath.Join(repoRoot, "scripts", "create-new-version.js")
	writeReducerTestFile(t, scriptPath, `const updateCurrentSpecsFiles = () => {
  const pathFiles = [];
  for (const file of pathFiles) {
    const yaml = {};
    const findAndUpdate = (obj) => {
      Object.keys(obj).forEach((key) => {
        if (obj[key] instanceof Object) {
          findAndUpdate(obj[key]);
        }
      });
    };
    findAndUpdate(yaml);
  }
};

const updateNextSpecsFiles = () => {
  const pathFiles = [];
  for (const file of pathFiles) {
    const yaml = {};
    const findAndUpdate = (obj) => {
      Object.keys(obj).forEach((key) => {
        if (obj[key] instanceof Object) {
          findAndUpdate(obj[key]);
        }
      });
    };
    findAndUpdate(yaml);
  }
};
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
	assignReducerTestFunctionUID(t, payload, "updateCurrentSpecsFiles", "content-entity:update-current")
	assignReducerTestFunctionUID(t, payload, "updateNextSpecsFiles", "content-entity:update-next")
	assignReducerTestDuplicateFunctionUIDs(t, payload, "findAndUpdate", "content-entity:current-helper", "content-entity:next-helper")

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
				"relative_path":    reducerTestRelativePath(t, repoRoot, scriptPath),
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "content-entity:update-current", "content-entity:current-helper")
	assertReducerCodeCallRow(t, rows, "content-entity:update-next", "content-entity:next-helper")
}

func TestExtractCodeCallRowsResolvesTypeScriptClassPropertyReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "service.ts")
	calleePath := filepath.Join(repoRoot, "server", "worker.ts")
	writeReducerTestFile(t, callerPath, `import { Worker } from "./worker";

export class SearchService {
  private readonly worker: Worker;

  constructor(worker: Worker) {
    this.worker = worker;
  }

  async searchText() {
    await this.worker.invoke();
  }
}
`)
	writeReducerTestFile(t, calleePath, `export class Worker {
  async invoke() {
    return "ok";
  }
}
`)

	rows := parsedJavaScriptCodeCallRows(t, repoRoot, callerPath, "searchText", calleePath, "invoke")
	assertReducerCodeCallRow(t, rows, "content-entity:searchText", "content-entity:invoke")
}

func TestExtractCodeCallRowsResolvesTypeScriptFactoryReturnReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "handler.ts")
	calleePath := filepath.Join(repoRoot, "server", "SearchService.ts")
	writeReducerTestFile(t, callerPath, `import { SearchService } from "./SearchService";

function getSearchService(): SearchService {
  return new SearchService();
}

export const post = () => {
  const searchService = getSearchService();
  return searchService.searchText();
};
`)
	writeReducerTestFile(t, calleePath, `export class SearchService {
  searchText() {
    return "ok";
  }
}
`)

	rows := parsedJavaScriptCodeCallRows(t, repoRoot, callerPath, "post", calleePath, "searchText")
	assertReducerCodeCallRow(t, rows, "content-entity:post", "content-entity:searchText")
}

func TestExtractCodeCallRowsResolvesTypeScriptThisMethodCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	servicePath := filepath.Join(repoRoot, "server", "SearchService.ts")
	writeReducerTestFile(t, servicePath, `export class SearchService {
  searchText() {
    return this.extractStructuredResults();
  }

  private extractStructuredResults() {
    return [];
  }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{servicePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, servicePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(service) error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, payload, "searchText", "content-entity:searchText")
	assignReducerTestFunctionUID(t, payload, "extractStructuredResults", "content-entity:extractStructuredResults")

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
				"relative_path":    reducerTestRelativePath(t, repoRoot, servicePath),
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "content-entity:searchText", "content-entity:extractStructuredResults")
}

func assignReducerTestDuplicateFunctionUIDs(t *testing.T, payload map[string]any, name string, uids ...string) {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	seen := 0
	for _, function := range functions {
		if function["name"] != name {
			continue
		}
		if seen >= len(uids) {
			t.Fatalf("more %q functions than uids; functions=%#v", name, functions)
		}
		function["uid"] = uids[seen]
		seen++
	}
	if seen != len(uids) {
		t.Fatalf("assigned %d %q functions, want %d; functions=%#v", seen, name, len(uids), functions)
	}
}

func assertReducerCodeCallRow(t *testing.T, rows []map[string]any, callerID string, calleeID string) {
	t.Helper()

	for _, row := range rows {
		if row["caller_entity_id"] == callerID && row["callee_entity_id"] == calleeID {
			return
		}
	}
	t.Fatalf("missing row %s -> %s in %#v", callerID, calleeID, rows)
}

func assertReducerNoCodeCallRow(t *testing.T, rows []map[string]any, callerID string, calleeID string) {
	t.Helper()

	for _, row := range rows {
		if row["caller_entity_id"] == callerID && row["callee_entity_id"] == calleeID {
			t.Fatalf("unexpected row %s -> %s in %#v", callerID, calleeID, rows)
		}
	}
}
