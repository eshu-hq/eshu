package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathTypeScriptHapiHandlerExportAssignmentRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	specsPath := filepath.Join(repoRoot, "server", "init", "plugins", "specs.ts")
	handlerPath := filepath.Join(repoRoot, "server", "handlers", "admin", "block-lists-api.ts")
	writeTestFile(
		t,
		specsPath,
		`import path from "path";

export const options = {
  openapi: {
    handlers: path.resolve(__dirname, "../../handlers"),
  },
};
`,
	)
	writeTestFile(
		t,
		handlerPath,
		`const get = async () => "get";
const post = async () => "post";
const deleteHandler = async () => "delete";
const helper = async () => "helper";

export = {
  get,
  post,
  delete: deleteHandler,
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, handlerPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(handler) error = %v, want nil", err)
	}

	for _, name := range []string{"get", "post", "deleteHandler"} {
		assertParserStringSliceFieldValue(
			t,
			assertFunctionByName(t, payload, name),
			"dead_code_root_kinds",
			[]string{"javascript.hapi_handler_export"},
		)
	}
	helperItem := assertFunctionByName(t, payload, "helper")
	if _, ok := helperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for unexported helper", helperItem["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathTypeScriptHapiDefaultExportPluginRegisterRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	pluginPath := filepath.Join(repoRoot, "server", "init", "plugins", "feature-flags.ts")
	writeTestFile(
		t,
		pluginPath,
		`function helper() {
  return false;
}

function register(server): void {
  server.expose("ready", helper);
}

export default {
  name: "feature-flags",
  register,
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, pluginPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(plugin) error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, payload, "register"),
		"dead_code_root_kinds",
		[]string{"javascript.hapi_plugin_register"},
	)
	helperItem := assertFunctionByName(t, payload, "helper")
	if _, ok := helperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for plugin helper", helperItem["dead_code_root_kinds"])
	}
}
