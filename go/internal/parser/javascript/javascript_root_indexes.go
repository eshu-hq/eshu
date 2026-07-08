// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaScriptRootIndexes bundles the four independent per-file lookup
// structures Parse's main dispatch walk needs: react wrapper aliases,
// CommonJS module.exports variable aliases, inferred variable/parameter/field
// receiver types, and Fastify app-instance variable names. Each builder reads
// only root, source (and, for Fastify, the raw source text), and none
// consumes another's completed output, so buildJavaScriptRootIndexes computes
// all four in one shared traversal instead of the four independent full-tree
// walks javaScriptReactAliases, javaScriptCommonJSModuleExportAliases,
// javaScriptNewExpressionVariableTypes, and javaScriptFastifyRegistrationBases
// would otherwise perform.
type javaScriptRootIndexes struct {
	reactAliases          map[string]string
	commonJSModuleAliases map[string]struct{}
	newExpressionTypes    map[string]string
	fastifyBases          map[string]struct{}
}

// buildJavaScriptRootIndexes computes javaScriptRootIndexes for one parsed
// file. outputLanguage gates react alias collection to tsx (matching
// javaScriptReactAliases); sourceText gates Fastify base collection to files
// with a Fastify import (matching javaScriptFastifyRegistrationBases), so
// files without either signal skip that collector's per-node work inside the
// shared walk exactly as the original standalone functions skipped their own
// walk entirely.
//
// javaScriptFunctionReturnTypes still runs as its own preliminary full-tree
// walk: javaScriptCollectNewExpressionVariableType requires it fully computed
// before any node is visited, since a variable's call_expression value may
// reference a function declared later in the file.
func buildJavaScriptRootIndexes(
	root *tree_sitter.Node,
	source []byte,
	sourceText string,
	outputLanguage string,
) javaScriptRootIndexes {
	wantReactAliases := outputLanguage == "tsx"
	wantFastifyBases := javaScriptHasFastifyImport(sourceText)

	reactAliases := map[string]string{}
	commonJSModuleAliases := make(map[string]struct{})
	returnTypesByFunction := javaScriptFunctionReturnTypes(root, source)
	newExpressionTypes := make(map[string]string)
	fastifyBases := make(map[string]struct{})

	walkNamed(root, func(node *tree_sitter.Node) {
		if wantReactAliases {
			javaScriptCollectReactAliasFromImportStatement(node, source, outputLanguage, reactAliases)
		}
		javaScriptCollectCommonJSModuleExportAlias(node, source, commonJSModuleAliases)
		javaScriptCollectNewExpressionVariableType(node, source, returnTypesByFunction, newExpressionTypes)
		if wantFastifyBases {
			javaScriptCollectFastifyRegistrationBase(node, source, fastifyBases)
		}
	})

	if len(reactAliases) == 0 {
		reactAliases = nil
	}
	return javaScriptRootIndexes{
		reactAliases:          reactAliases,
		commonJSModuleAliases: commonJSModuleAliases,
		newExpressionTypes:    newExpressionTypes,
		fastifyBases:          fastifyBases,
	}
}
