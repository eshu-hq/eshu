package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaScriptNodeEntrypointsMapCompiledPackageTargetsToSourceRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	packageRoot := filepath.Join(repoRoot, "packages", "worker")
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "workspace-root",
  "main": "packages/worker/src/root.ts",
  "workspaces": ["packages/*"]
}`)
	writeTestFile(t, filepath.Join(packageRoot, "package.json"), `{
  "name": "@example/worker",
  "main": "lib/index.js",
  "exports": {
    "./workers/*": {
      "import": "./dist/workers/*.js"
    }
  }
}`)

	indexPath := filepath.Join(packageRoot, "src", "index.ts")
	exportPath := filepath.Join(packageRoot, "src", "workers", "sync.ts")
	rootOwnedPath := filepath.Join(packageRoot, "src", "root.ts")
	writeTestFile(t, indexPath, `export function bootstrap() {
  return "ready";
}

function helper() {
  return "local";
}
`)
	writeTestFile(t, exportPath, `export function syncWorker() {
  return "sync";
}

function localWorkerHelper() {
  return "local";
}
`)
	writeTestFile(t, rootOwnedPath, `export function bootstrap() {
  return "root";
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	indexPayload, err := engine.ParsePath(repoRoot, indexPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(index) error = %v, want nil", err)
	}
	exportPayload, err := engine.ParsePath(repoRoot, exportPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(export) error = %v, want nil", err)
	}
	rootOwnedPayload, err := engine.ParsePath(repoRoot, rootOwnedPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(root-owned) error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, indexPayload, "bootstrap"),
		"dead_code_root_kinds",
		"javascript.node_package_entrypoint",
	)
	if _, ok := assertFunctionByName(t, indexPayload, "helper")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent for package entrypoint helper")
	}
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, exportPayload, "syncWorker"),
		"dead_code_root_kinds",
		"javascript.node_package_export",
	)
	if _, ok := assertFunctionByName(t, exportPayload, "localWorkerHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("localWorkerHelper dead_code_root_kinds present, want absent for non-exported package export helper")
	}
	if _, ok := assertFunctionByName(t, rootOwnedPayload, "bootstrap")["dead_code_root_kinds"]; ok {
		t.Fatalf("root-owned bootstrap dead_code_root_kinds present, want nested package manifest to own package scope")
	}
}

func TestDefaultEngineParsePathJavaScriptNodeEntrypointsMapBinAndExtensionlessScriptTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "bin": {
    "sample-cli": "./dist/bin/sample.js"
  },
  "scripts": {
    "worker": "NODE_ENV=production node --import tsx ./src/jobs/sync"
  }
}`)

	binPath := filepath.Join(repoRoot, "src", "bin", "sample.ts")
	scriptPath := filepath.Join(repoRoot, "src", "jobs", "sync.ts")
	writeTestFile(t, binPath, `export function runCli() {
  return "cli";
}
`)
	writeTestFile(t, scriptPath, `async function run() {
  return "ok";
}

function helper() {
  return "local";
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	binPayload, err := engine.ParsePath(repoRoot, binPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(bin) error = %v, want nil", err)
	}
	scriptPayload, err := engine.ParsePath(repoRoot, scriptPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(script) error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, binPayload, "runCli"),
		"dead_code_root_kinds",
		"javascript.node_package_bin",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, scriptPayload, "run"),
		"dead_code_root_kinds",
		"javascript.node_package_script",
	)
	if _, ok := assertFunctionByName(t, scriptPayload, "helper")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent for package script helper")
	}
}
