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

func TestDefaultEngineParsePathTypeScriptMarksPublicImportedExportClauseSurface(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "fastify-shaped",
  "types": "./fastify.d.ts"
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "fastify.d.ts"), `import { PublicRequest, InternalReply as PublicReply } from "./types/request";
import { PublicSchema } from "./types/schema";

declare namespace fastify {
  export type {
    PublicRequest,
    PublicReply,
    PublicSchema
  }
}
`)
	requestPath := filepath.Join(repoRoot, "types", "request.d.ts")
	writeTestFile(t, requestPath, `export interface PublicRequest {
  id: string;
}

export interface InternalReply {
  statusCode: number;
}

export interface LocalOnly {
  hidden: boolean;
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "types", "schema.d.ts"), `export interface PublicSchema {
  body?: unknown;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, requestPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, got, "interfaces", "PublicRequest"),
		"dead_code_root_kinds",
		"typescript.public_api_reexport",
	)
	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, got, "interfaces", "InternalReply"),
		"dead_code_root_kinds",
		"typescript.public_api_reexport",
	)
	if _, ok := assertBucketItemByName(t, got, "interfaces", "LocalOnly")["dead_code_root_kinds"]; ok {
		t.Fatalf("LocalOnly dead_code_root_kinds present, want absent outside imported export clause")
	}
}

func TestDefaultEngineParsePathTypeScriptMarksPublicImportedExportClauseWithTrailingComment(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "fastify-comment-shaped",
  "types": "./fastify.d.ts"
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "fastify.d.ts"), `import { FastifyContextConfig, FastifyReplyContext, FastifyRequestContext } from "./types/context";

declare namespace fastify {
  export type {
    FastifyRequestContext, FastifyContextConfig, FastifyReplyContext, // "./types/context"
  }
}
`)
	contextPath := filepath.Join(repoRoot, "types", "context.d.ts")
	writeTestFile(t, contextPath, `export interface FastifyContextConfig {
}

export interface FastifyRequestContext {
  config: FastifyContextConfig;
}

export interface FastifyReplyContext {
  config: FastifyContextConfig;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, contextPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	for _, name := range []string{"FastifyRequestContext", "FastifyContextConfig", "FastifyReplyContext"} {
		assertParserStringSliceContains(
			t,
			assertBucketItemByName(t, got, "interfaces", name),
			"dead_code_root_kinds",
			"typescript.public_api_reexport",
		)
	}
}

func TestDefaultEngineParsePathTypeScriptMarksFastifyDeclarationPublicSurface(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "fastify-real-shape",
  "types": "fastify.d.ts"
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "fastify.d.ts"), `import { FastifyContextConfig, FastifyReplyContext, FastifyRequestContext } from './types/context'
import { FastifyRequest, RequestGenericInterface } from './types/request'
import { FastifySchema, FastifySchemaValidationError, FastifySchemaCompiler, FastifySerializerCompiler } from './types/schema'
import { FastifyTypeProvider, FastifyTypeProviderDefault, SafePromiseLike } from './types/type-provider'

declare namespace fastify {
  export type {
    FastifyRequest, RequestGenericInterface, // './types/request'
    FastifyRequestContext, FastifyContextConfig, FastifyReplyContext, // './types/context'
    FastifySchema, FastifySchemaValidationError, FastifySchemaCompiler, FastifySerializerCompiler, // './types/schema'
    FastifyTypeProvider, FastifyTypeProviderDefault, SafePromiseLike, // './types/type-provider'
  }
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "types", "request.d.ts"), `import { FastifySchema } from './schema'
import { FastifyRequestType, FastifyTypeProvider, FastifyTypeProviderDefault, ResolveFastifyRequestType } from './type-provider'

export interface RequestGenericInterface {
  Body?: unknown;
}

export interface FastifyRequest<RouteGeneric extends RequestGenericInterface = RequestGenericInterface,
  SchemaCompiler extends FastifySchema = FastifySchema,
  TypeProvider extends FastifyTypeProvider = FastifyTypeProviderDefault,
  RequestType extends FastifyRequestType = ResolveFastifyRequestType<TypeProvider, SchemaCompiler, RouteGeneric>
> {
  body: RequestType['body'];
}
`)
	contextPath := filepath.Join(repoRoot, "types", "context.d.ts")
	writeTestFile(t, contextPath, `import { FastifyRouteConfig } from './route'
import { ContextConfigDefault } from './utils'

export interface FastifyContextConfig {
}

export interface FastifyRequestContext<ContextConfig = ContextConfigDefault> {
  config: FastifyContextConfig & FastifyRouteConfig & ContextConfig;
}

export interface FastifyReplyContext<ContextConfig = ContextConfigDefault> {
  config: FastifyContextConfig & FastifyRouteConfig & ContextConfig;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, contextPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	for _, name := range []string{"FastifyContextConfig", "FastifyRequestContext", "FastifyReplyContext"} {
		assertParserStringSliceContains(
			t,
			assertBucketItemByName(t, got, "interfaces", name),
			"dead_code_root_kinds",
			"typescript.public_api_reexport",
		)
	}

	typeProviderPath := filepath.Join(repoRoot, "types", "type-provider.d.ts")
	writeTestFile(t, typeProviderPath, `import { FastifySchema } from './schema'

export interface FastifyTypeProvider {
  readonly schema: unknown,
}

export interface FastifyTypeProviderDefault extends FastifyTypeProvider {}

export interface FastifyRequestType<Body = unknown> {
  body: Body
}

export interface ResolveFastifyRequestType<TypeProvider extends FastifyTypeProvider, SchemaCompiler extends FastifySchema, RouteGeneric extends RequestGenericInterface> extends FastifyRequestType {
  body: unknown
}
`)
	got, err = engine.ParsePath(repoRoot, typeProviderPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(type-provider) error = %v, want nil", err)
	}
	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, got, "interfaces", "ResolveFastifyRequestType"),
		"dead_code_root_kinds",
		"typescript.public_api_type_reference",
	)
}

func TestDefaultEngineParsePathTypeScriptDoesNotFollowDeclarationBarrelsOutsideRepo(t *testing.T) {
	t.Parallel()

	parentDir := t.TempDir()
	repoRoot := filepath.Join(parentDir, "repo")
	outsideRoot := filepath.Join(parentDir, "outside")
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "@example/escape",
  "types": "./lib/index.d.ts"
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "lib", "index.d.ts"), `export * from "../../outside/barrel";
`)
	writeTestFile(t, filepath.Join(outsideRoot, "barrel.d.ts"), `export * from "../repo/lib/private";
`)
	privatePath := filepath.Join(repoRoot, "lib", "private.d.ts")
	writeTestFile(t, privatePath, `export interface InternalOnly {
  value: string;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, privatePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if item := assertBucketItemByName(t, got, "interfaces", "InternalOnly"); item["dead_code_root_kinds"] != nil {
		t.Fatalf("InternalOnly dead_code_root_kinds = %#v, want nil for outside-repo barrel", item["dead_code_root_kinds"])
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
