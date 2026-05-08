package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaScriptPackageDeadCodeRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(
		t,
		filepath.Join(repoRoot, "package.json"),
		`{
  "name": "service-sample",
  "main": "dist/service-sample.js",
  "module": "dist/service-sample.mjs",
  "scripts": {
    "start": "node dist/service-sample.js",
    "dev": "tsx service-sample.ts"
  },
  "bin": {
    "service-sample": "dist/cli.js"
  },
  "exports": {
    ".": "./dist/server/public-api.js"
  }
}`,
	)
	entryPath := filepath.Join(repoRoot, "service-sample.ts")
	binPath := filepath.Join(repoRoot, "cli.ts")
	publicPath := filepath.Join(repoRoot, "server", "public-api.ts")
	privatePath := filepath.Join(repoRoot, "server", "private-helper.ts")
	writeTestFile(
		t,
		entryPath,
		`function bootstrap() {
  return "ready";
}

function entryHelper() {
  return "helper";
}
`,
	)
	writeTestFile(
		t,
		binPath,
		`function runCli() {
  return "cli";
}
`,
	)
	writeTestFile(
		t,
		publicPath,
		`export function publicApi() {
  return "api";
}

export class PublicClient {
  request() {
    return "ok";
  }
}

function privatePublicFileHelper() {
  return "helper";
}
`,
	)
	writeTestFile(
		t,
		privatePath,
		`function unusedPrivateHelper() {
  return "unused";
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	entryPayload, err := engine.ParsePath(repoRoot, entryPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(entry) error = %v, want nil", err)
	}
	binPayload, err := engine.ParsePath(repoRoot, binPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(bin) error = %v, want nil", err)
	}
	publicPayload, err := engine.ParsePath(repoRoot, publicPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(public) error = %v, want nil", err)
	}
	privatePayload, err := engine.ParsePath(repoRoot, privatePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(private) error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, entryPayload, "bootstrap"),
		"dead_code_root_kinds",
		"javascript.node_package_entrypoint",
	)
	if _, ok := assertFunctionByName(t, entryPayload, "entryHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("entryHelper dead_code_root_kinds present, want absent for non-exported entrypoint helper")
	}
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, binPayload, "runCli"),
		"dead_code_root_kinds",
		"javascript.node_package_bin",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, publicPayload, "publicApi"),
		"dead_code_root_kinds",
		"javascript.node_package_export",
	)
	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, publicPayload, "classes", "PublicClient"),
		"dead_code_root_kinds",
		"javascript.node_package_export",
	)
	if _, ok := assertFunctionByName(t, publicPayload, "privatePublicFileHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("privatePublicFileHelper dead_code_root_kinds present, want absent for non-exported helper")
	}
	if _, ok := assertFunctionByName(t, privatePayload, "unusedPrivateHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("unusedPrivateHelper dead_code_root_kinds present, want absent outside package public roots")
	}
}

func TestDefaultEngineParsePathJavaScriptHapiHandlerRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(
		t,
		filepath.Join(repoRoot, "package.json"),
		`{"name":"service-hapi"}`,
	)
	writeTestFile(
		t,
		filepath.Join(repoRoot, "server", "init", "plugins", "specs.ts"),
		`import { plugin } from '@example/hapi-service/init/plugins/specs';
import path from 'path';

export const options = {
  openapi: {
    handlers: path.resolve(__dirname, '../../handlers'),
  },
};

export default { plugin, options };
`,
	)
	esModuleHandlerPath := filepath.Join(repoRoot, "server", "handlers", "chat", "response.ts")
	commonJSHandlerPath := filepath.Join(repoRoot, "server", "handlers", "_status.js")
	resourcePath := filepath.Join(repoRoot, "server", "resources", "status.ts")
	writeTestFile(
		t,
		esModuleHandlerPath,
		`export const post = async (request) => request.payload;

const localHelper = () => "helper";
`,
	)
	writeTestFile(
		t,
		commonJSHandlerPath,
		`module.exports.get = async () => ({ statusCode: 200 });

module.exports.payload = async () => ({ message: "ok" });
`,
	)
	writeTestFile(
		t,
		resourcePath,
		`export const get = async () => "not a handler";
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	esModulePayload, err := engine.ParsePath(repoRoot, esModuleHandlerPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(es module handler) error = %v, want nil", err)
	}
	commonJSPayload, err := engine.ParsePath(repoRoot, commonJSHandlerPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(commonjs handler) error = %v, want nil", err)
	}
	resourcePayload, err := engine.ParsePath(repoRoot, resourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(resource) error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, esModulePayload, "post"),
		"dead_code_root_kinds",
		"javascript.hapi_handler_export",
	)
	if _, ok := assertFunctionByName(t, esModulePayload, "localHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("localHelper dead_code_root_kinds present, want absent for non-exported handler helper")
	}
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, commonJSPayload, "get"),
		"dead_code_root_kinds",
		"javascript.hapi_handler_export",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, commonJSPayload, "payload"),
		"dead_code_root_kinds",
		"javascript.hapi_handler_export",
	)
	if _, ok := assertFunctionByName(t, resourcePayload, "get")["dead_code_root_kinds"]; ok {
		t.Fatalf("resource get dead_code_root_kinds present, want absent outside configured handlers")
	}
}
