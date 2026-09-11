// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

// EntityIndex is the built lookup structure [BuildEntityIndex] returns. It
// resolves parsed code entities (functions, classes, structs, interfaces,
// type aliases) by path/line, by name uniqueness at various scopes, and by
// stable symbol key, backing call, route-handler, and cloud-action resolution
// across the reducer root. The zero value has nil maps and resolves nothing.
type EntityIndex struct {
	entitiesByPathLine map[string]string
	spansByPath        map[string][]codeFunctionSpan
	containersByPath   map[string][]codeFunctionSpan
	// UniqueNameByPath maps a normalized file path key to the function/type
	// names that resolve to exactly one entity within that path. A name absent
	// from the inner map was ambiguous (declared more than once) in that file
	// and must not be resolved from it.
	UniqueNameByPath map[string]map[string]string
	// UniqueNameByRepo maps a repository ID to the function/type names that
	// resolve to exactly one entity across the whole repository. A name absent
	// from the inner map was ambiguous repository-wide and must not be
	// resolved from it.
	UniqueNameByRepo                 map[string]map[string]string
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
	// repositoryImportPathsByRepo caches the normalized, deduplicated path set
	// used by unresolved JavaScript and Python import-binding barriers. Building
	// it once per extraction avoids walking every repository import for each call.
	repositoryImportPathsByRepo map[string][]string
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
