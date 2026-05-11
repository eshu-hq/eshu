package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathTypeScriptMarksPackageBarrelReExportSurface(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "@example/library",
  "exports": {
    ".": "./src/index.ts"
  }
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "src", "index.ts"), `export { createClient } from "./client";
`)
	clientPath := filepath.Join(repoRoot, "src", "client.ts")
	writeTestFile(t, clientPath, `export interface ClientOptions {
  endpoint: string;
}

export function createClient(options: ClientOptions) {
  return options.endpoint;
}

function normalizeOptions() {
  return {};
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, clientPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "createClient"),
		"dead_code_root_kinds",
		"typescript.public_api_reexport",
	)
	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, got, "interfaces", "ClientOptions"),
		"dead_code_root_kinds",
		"typescript.public_api_type_reference",
	)
	if _, ok := assertFunctionByName(t, got, "normalizeOptions")["dead_code_root_kinds"]; ok {
		t.Fatalf("normalizeOptions dead_code_root_kinds present, want absent for private helper")
	}
}

func TestDefaultEngineParsePathTypeScriptMarksDeclarationOnlyTypeBarrelSurface(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "@example/contracts",
  "types": "./src/index.d.ts"
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "src", "index.d.ts"), `export type { PublicConfig } from "./config";
`)
	configPath := filepath.Join(repoRoot, "src", "config.ts")
	writeTestFile(t, configPath, `export interface PublicConfig {
  region: string;
}

export interface InternalConfig {
  token: string;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, configPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, got, "interfaces", "PublicConfig"),
		"dead_code_root_kinds",
		"typescript.public_api_reexport",
	)
	if _, ok := assertBucketItemByName(t, got, "interfaces", "InternalConfig")["dead_code_root_kinds"]; ok {
		t.Fatalf("InternalConfig dead_code_root_kinds present, want absent outside declaration barrel")
	}
}

func TestDefaultEngineParsePathTypeScriptMarksMultiHopDeclarationBarrelSurface(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "@example/hapi-shape",
  "types": "./lib/index.d.ts"
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "lib", "index.d.ts"), `export * from "./types";
`)
	writeTestFile(t, filepath.Join(repoRoot, "lib", "types", "index.d.ts"), `export * from "./plugin";
export * from "./route";
`)
	pluginPath := filepath.Join(repoRoot, "lib", "types", "plugin.d.ts")
	writeTestFile(t, pluginPath, `export interface PluginRegistered {
  name: string;
}

interface InternalPluginState {
  loaded: boolean;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, pluginPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, got, "interfaces", "PluginRegistered"),
		"dead_code_root_kinds",
		"typescript.public_api_reexport",
	)
	if _, ok := assertBucketItemByName(t, got, "interfaces", "InternalPluginState")["dead_code_root_kinds"]; ok {
		t.Fatalf("InternalPluginState dead_code_root_kinds present, want absent outside exported declaration surface")
	}
}

func TestDefaultEngineParsePathTypeScriptMarksPackageBarrelTSConfigPathReExportSurface(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "@example/path-surface",
  "exports": {
    ".": "./src/index.ts"
  }
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "@surface/*": ["src/*"]
    }
  }
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "src", "index.ts"), `export { buildFeature } from "@surface/feature";
`)
	featurePath := filepath.Join(repoRoot, "src", "feature.ts")
	writeTestFile(t, featurePath, `export function buildFeature() {
  return true;
}

export function internalFeature() {
  return false;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, featurePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "buildFeature"),
		"dead_code_root_kinds",
		"typescript.public_api_reexport",
	)
	if _, ok := assertFunctionByName(t, got, "internalFeature")["dead_code_root_kinds"]; ok {
		t.Fatalf("internalFeature dead_code_root_kinds present, want absent outside tsconfig path barrel")
	}
}

func TestDefaultEngineParsePathTypeScriptMarksExportedStaticRegistryMembers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	registryPath := filepath.Join(repoRoot, "src", "registry.ts")
	writeTestFile(t, registryPath, `type EventHandler = (payload: string) => string;

function dispatchRegisteredEvent(payload: string): string {
  return payload.toUpperCase();
}

function localHelper(payload: string): string {
  return payload.trim();
}

export const staticEventRegistry: Record<string, EventHandler> = {
  account_created: dispatchRegisteredEvent,
};

const privateRegistry: Record<string, EventHandler> = {
  local: localHelper,
};
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, registryPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "dispatchRegisteredEvent"),
		"dead_code_root_kinds",
		"typescript.static_registry_member",
	)
	if _, ok := assertFunctionByName(t, got, "localHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("localHelper dead_code_root_kinds present, want absent for private registry member")
	}
}
