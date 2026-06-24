// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// parseTypeScriptRootForTest parses TypeScript source into a root node and source
// bytes for exercising the AST-based dead-code surface helpers. The caller must
// invoke close once finished.
func parseTypeScriptRootForTest(t *testing.T, source string) (*tree_sitter.Node, []byte, func()) {
	t.Helper()
	language := tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	bytes := []byte(source)
	tree := parser.Parse(bytes, nil)
	if tree == nil {
		parser.Close()
		t.Fatalf("Parse() returned nil tree")
	}
	return tree.RootNode(), bytes, func() {
		tree.Close()
		parser.Close()
	}
}

func TestTypeScriptImportedExportClauseReexportsFromRootHandlesFastifyShape(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `import { FastifyContextConfig, FastifyReplyContext, FastifyRequestContext } from './types/context'
import { FastifyRequest, RequestGenericInterface } from './types/request'

declare namespace fastify {
  export type {
    FastifyRequest, RequestGenericInterface, // './types/request'
    FastifyRequestContext, FastifyContextConfig, FastifyReplyContext, // './types/context'
  }
}
`)
	defer closeFn()

	got := javaScriptTypeScriptImportedExportClauseReexportsFromRoot(root, source)
	want := map[string]string{
		"FastifyRequest":          "./types/request",
		"RequestGenericInterface": "./types/request",
		"FastifyRequestContext":   "./types/context",
		"FastifyContextConfig":    "./types/context",
		"FastifyReplyContext":     "./types/context",
	}
	for name, moduleSource := range want {
		assertTypeScriptImportedExportReexport(t, got, name, name, moduleSource)
	}
}

func TestTypeScriptImportedExportClauseReexportsFromRootHandlesDefaultAndNamedImport(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `import Fastify, { type FastifyRequest, InternalReply as FastifyReply } from './types/request'

declare namespace fastify {
  export type {
    FastifyRequest,
    FastifyReply
  }
}
`)
	defer closeFn()

	got := javaScriptTypeScriptImportedExportClauseReexportsFromRoot(root, source)
	assertTypeScriptImportedExportReexport(t, got, "FastifyRequest", "FastifyRequest", "./types/request")
	assertTypeScriptImportedExportReexport(t, got, "FastifyReply", "InternalReply", "./types/request")
}

func TestTypeScriptImportedExportClauseReexportsFromRootIgnoresBlockComments(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `import { FastifyRequest, InternalReply as FastifyReply } from './types/request'

declare namespace fastify {
  export type {
    FastifyRequest /* public request type */,
    FastifyReply /* public reply type */
  }
}
`)
	defer closeFn()

	got := javaScriptTypeScriptImportedExportClauseReexportsFromRoot(root, source)
	assertTypeScriptImportedExportReexport(t, got, "FastifyRequest", "FastifyRequest", "./types/request")
	assertTypeScriptImportedExportReexport(t, got, "FastifyReply", "InternalReply", "./types/request")
}

func TestTypeScriptImportedTypeReferencesFromPublicDeclarations(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `import { FastifyRequestType, ResolveFastifyRequestType } from './type-provider'

export interface FastifyRequest<RequestType extends FastifyRequestType = ResolveFastifyRequestType> {
  body: RequestType['body'];
}
`)
	defer closeFn()

	item := javaScriptTypeScriptSurfaceWalkItem{
		names: map[string]struct{}{"FastifyRequest": {}},
	}
	bindings := map[string]javaScriptTypeScriptImportedBinding{
		"ResolveFastifyRequestType": {
			importedName: "ResolveFastifyRequestType",
			source:       "./type-provider",
		},
	}

	got := javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations(root, source, item, bindings)
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
