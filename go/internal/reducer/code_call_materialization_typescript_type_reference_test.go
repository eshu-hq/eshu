package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesTypeScriptTypeReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "resources", "client.ts")
	calleePath := filepath.Join(repoRoot, "server", "resources", "types.ts")
	writeReducerTestFile(t, callerPath, `import type { ClientOptions } from "./types";

export class Client {
  constructor(options: ClientOptions) {
    this.options = options;
  }
}
`)
	writeReducerTestFile(t, calleePath, `export interface ClientOptions {
  apiKey: string;
}

export interface LocationModeCoordinates {
  lat: string;
  lon: string;
}

export interface LocationMode {
  coordinates?: LocationModeCoordinates;
}

export interface SearchRequest {
  locationMode?: LocationMode;
}
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
	assignReducerTestFunctionUID(t, callerPayload, "constructor", "content-entity:constructor")
	assignReducerTestInterfaceUID(t, calleePayload, "ClientOptions", "content-entity:client-options")
	assignReducerTestInterfaceUID(t, calleePayload, "LocationModeCoordinates", "content-entity:location-mode-coordinates")
	assignReducerTestInterfaceUID(t, calleePayload, "LocationMode", "content-entity:location-mode")
	assignReducerTestInterfaceUID(t, calleePayload, "SearchRequest", "content-entity:search-request")

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
	})

	assertReducerCodeCallRow(t, rows, "content-entity:constructor", "content-entity:client-options")
	assertReducerCodeCallRow(t, rows, "content-entity:location-mode", "content-entity:location-mode-coordinates")
	assertReducerCodeCallRow(t, rows, "content-entity:search-request", "content-entity:location-mode")
}

func TestExtractCodeCallRowsResolvesTypeScriptTypeAssertionReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "plugin.ts")
	writeReducerTestFile(t, filePath, `interface AppConfig {
  services?: Record<string, string>;
}

interface ServerWithConfig {
  app?: AppConfig;
}

export function register(server: unknown) {
  const typed = server as ServerWithConfig;
  return typed.app as AppConfig | undefined;
}
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
	assignReducerTestFunctionUID(t, payload, "register", "content-entity:register")
	assignReducerTestInterfaceUID(t, payload, "AppConfig", "content-entity:app-config")
	assignReducerTestInterfaceUID(t, payload, "ServerWithConfig", "content-entity:server-with-config")

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
				"relative_path":    reducerTestRelativePath(t, repoRoot, filePath),
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "content-entity:register", "content-entity:app-config")
	assertReducerCodeCallRow(t, rows, "content-entity:register", "content-entity:server-with-config")
}

func TestExtractCodeCallRowsResolvesTypeScriptObjectMethodTypeAssertionReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "init", "plugins", "services.ts")
	writeReducerTestFile(t, filePath, `interface AppConfig {
  services?: Record<string, string>;
}

interface ServerWithConfigAndServices {
  app: {
    services?: Record<string, string>;
  };
}

const plugin = {
  register: (server: unknown) => {
    const serverWithConfig = server as ServerWithConfigAndServices;
    const appConfig = serverWithConfig.app as AppConfig | undefined;
    return appConfig;
  },
};

export default { plugin };
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
	assignReducerTestFunctionUID(t, payload, "register", "content-entity:register")
	assignReducerTestInterfaceUID(t, payload, "AppConfig", "content-entity:app-config")
	assignReducerTestInterfaceUID(t, payload, "ServerWithConfigAndServices", "content-entity:server-with-config")

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
				"relative_path":    reducerTestRelativePath(t, repoRoot, filePath),
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "content-entity:register", "content-entity:app-config")
	assertReducerCodeCallRow(t, rows, "content-entity:register", "content-entity:server-with-config")
}

func assignReducerTestInterfaceUID(t *testing.T, payload map[string]any, name string, uid string) {
	t.Helper()

	items, ok := payload["interfaces"].([]map[string]any)
	if !ok {
		t.Fatalf("interfaces = %T, want []map[string]any", payload["interfaces"])
	}
	for _, item := range items {
		if item["name"] == name {
			item["uid"] = uid
			return
		}
	}
	t.Fatalf("interface %q not found in %#v", name, items)
}
