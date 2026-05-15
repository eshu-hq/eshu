package parser

import (
	"path/filepath"
	"testing"
)

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
