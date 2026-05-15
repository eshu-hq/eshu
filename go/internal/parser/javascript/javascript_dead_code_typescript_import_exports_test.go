package javascript

import "testing"

func TestTypeScriptImportedExportClauseReexportsFromSourceHandlesFastifyShape(t *testing.T) {
	t.Parallel()

	source := `import { FastifyContextConfig, FastifyReplyContext, FastifyRequestContext } from './types/context'
import { FastifyRequest, RequestGenericInterface } from './types/request'

declare namespace fastify {
  export type {
    FastifyRequest, RequestGenericInterface, // './types/request'
    FastifyRequestContext, FastifyContextConfig, FastifyReplyContext, // './types/context'
  }
}
`

	got := javaScriptTypeScriptImportedExportClauseReexportsFromSource(source)
	want := map[string]string{
		"FastifyRequest":          "./types/request",
		"RequestGenericInterface": "./types/request",
		"FastifyRequestContext":   "./types/context",
		"FastifyContextConfig":    "./types/context",
		"FastifyReplyContext":     "./types/context",
	}
	for name, source := range want {
		assertTypeScriptImportedExportReexport(t, got, name, name, source)
	}
}

func TestTypeScriptImportedExportClauseReexportsFromSourceHandlesDefaultAndNamedImport(t *testing.T) {
	t.Parallel()

	source := `import Fastify, { type FastifyRequest, InternalReply as FastifyReply } from './types/request'

declare namespace fastify {
  export type {
    FastifyRequest,
    FastifyReply
  }
}
`

	got := javaScriptTypeScriptImportedExportClauseReexportsFromSource(source)
	assertTypeScriptImportedExportReexport(t, got, "FastifyRequest", "FastifyRequest", "./types/request")
	assertTypeScriptImportedExportReexport(t, got, "FastifyReply", "InternalReply", "./types/request")
}

func TestTypeScriptImportedExportClauseReexportsFromSourceIgnoresBlockComments(t *testing.T) {
	t.Parallel()

	source := `import { FastifyRequest, InternalReply as FastifyReply } from './types/request'

declare namespace fastify {
  export type {
    FastifyRequest /* public request type */,
    FastifyReply /* public reply type */
  }
}
`

	got := javaScriptTypeScriptImportedExportClauseReexportsFromSource(source)
	assertTypeScriptImportedExportReexport(t, got, "FastifyRequest", "FastifyRequest", "./types/request")
	assertTypeScriptImportedExportReexport(t, got, "FastifyReply", "InternalReply", "./types/request")
}

func TestTypeScriptImportedTypeReferencesFromPublicDeclarations(t *testing.T) {
	t.Parallel()

	source := `import { FastifyRequestType, ResolveFastifyRequestType } from './type-provider'

export interface FastifyRequest<RequestType extends FastifyRequestType = ResolveFastifyRequestType> {
  body: RequestType['body'];
}
`
	item := javaScriptTypeScriptSurfaceWalkItem{
		names: map[string]struct{}{"FastifyRequest": {}},
	}
	bindings := map[string]javaScriptTypeScriptImportedBinding{
		"ResolveFastifyRequestType": {
			importedName: "ResolveFastifyRequestType",
			source:       "./type-provider",
		},
	}

	if !javaScriptIdentifierMentioned(source, "ResolveFastifyRequestType") {
		t.Fatalf("source should mention ResolveFastifyRequestType")
	}
	got := javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations(source, item, bindings)
	if _, ok := got["ResolveFastifyRequestType"]; !ok {
		t.Fatalf("references = %#v, want ResolveFastifyRequestType", got)
	}
}

func assertTypeScriptImportedExportReexport(
	t *testing.T,
	reexports []javaScriptTypeScriptSurfaceReexport,
	exportedName string,
	originalName string,
	source string,
) {
	t.Helper()

	for _, reexport := range reexports {
		if reexport.exportedName == exportedName &&
			reexport.originalName == originalName &&
			reexport.source == source {
			return
		}
	}
	t.Fatalf("reexports missing %s/%s from %s: %#v", exportedName, originalName, source, reexports)
}
