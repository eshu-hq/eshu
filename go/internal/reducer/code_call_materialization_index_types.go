// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

type codeEntityIndex struct {
	entitiesByPathLine               map[string]string
	spansByPath                      map[string][]codeFunctionSpan
	containersByPath                 map[string][]codeFunctionSpan
	uniqueNameByPath                 map[string]map[string]string
	uniqueNameByRepo                 map[string]map[string]string
	uniqueNameByRepoDir              map[string]map[string]map[string]string
	constructorByPath                map[string]map[string]string
	goMethodReturnTypes              map[string]map[string]string
	rustTraitMethodsByRepo           map[string]map[string]string
	pythonClassBasesByRepo           map[string]map[string][]string
	entityFileByID                   map[string]string
	entityTypeByID                   map[string]string
	entityByStableSymbolKey          map[string]codeCallSymbolResolution
	javaScriptAliasesByPath          map[string][]javaScriptStaticAliasSpan
	typeScriptInterfaceMethodsByRepo map[string]map[string]map[string]string
	// receiverMethodsByRepo maps repositoryID -> receiver type -> method name ->
	// the single entity declaring that method on the type. It backs receiver-type
	// inferred call resolution for languages without dotted-import-to-file
	// mapping (Swift, JavaScript). Ambiguous methods are absent.
	receiverMethodsByRepo map[string]map[string]map[string]string
	// goExportByImportPath maps a Go package import path to the exported
	// top-level functions defined for it across every repository in the
	// generation. It anchors cross-repo package-export resolution: a key is the
	// caller-visible import path, and each entry tracks the single resolvable
	// entity plus a candidate count so ambiguity is detectable and rejected.
	goExportByImportPath map[string]map[string]goCrossRepoExportEntry
}

// goCrossRepoExportEntry records the unique-resolution state for one exported Go
// function name under one package import path. entityID and repositoryID are
// only safe to resolve when count == 1; a count above one marks the name
// ambiguous across repositories and forces an unresolved result.
type goCrossRepoExportEntry struct {
	entityID     string
	repositoryID string
	count        int
}

type codeFunctionSpan struct {
	startLine int
	endLine   int
	entityID  string
	names     []string
}

type javaScriptStaticAliasSpan struct {
	startLine int
	endLine   int
	aliases   javaScriptStaticAliasSet
}
