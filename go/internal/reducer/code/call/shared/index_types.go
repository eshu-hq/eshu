// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

// EntityIndex is the built lookup structure [BuildEntityIndex] returns. It
// resolves parsed code entities (functions, classes, structs, interfaces,
// type aliases) by path/line, by name uniqueness at various scopes, and by
// stable symbol key, backing call, route-handler, and cloud-action resolution
// across the reducer root. The zero value has nil maps and resolves nothing.
//
// The language-specific lookup fields stay unexported so the read-only
// invariant survives the package boundary; a per-language resolver package
// reads them through the accessor methods below instead of the raw fields.
type EntityIndex struct {
	// entitiesByRepoPathLine maps repositoryID -> "path#line" -> the entity
	// declared at that line of that file. A file is keyed only by its own
	// normalized full path and normalized relative path (see [FileKeys]);
	// never by a bare file name, so a path/line lookup cannot cross into a
	// same-named file of another directory or another repository (#7642).
	entitiesByRepoPathLine map[string]map[string]string
	// spansByFile maps repositoryID -> file key -> function spans in that
	// file. A file is keyed only by its own normalized full path and
	// normalized relative path (see [FileKeys]); never by a bare file name,
	// so a span lookup cannot cross into a same-named file of another
	// directory or another repository (#7640).
	spansByFile map[string]map[string][]FunctionSpan
	// containersByFile has the same repository and file keying as
	// spansByFile and holds function plus class/struct/interface/type-alias
	// spans, the candidates for a call's containing entity.
	containersByFile map[string]map[string][]FunctionSpan
	// uniqueNameByPath maps a normalized file path key to the function/type
	// names that resolve to exactly one entity within that path. A name absent
	// from the inner map was ambiguous (declared more than once) in that file
	// and must not be resolved from it. The keys aggregate every repository
	// in the generation, so this map backs only cross-file import lookups
	// that probe an explicitly matched target path; same-file lookups use
	// uniqueNameByRepoPath instead (#7642).
	uniqueNameByPath map[string]map[string]string
	// uniqueNameByRepoPath maps repositoryID -> file key -> the
	// function/type names that resolve to exactly one entity within that
	// file. A file is keyed only by its own normalized full path and
	// normalized relative path (see [FileKeys]); never by a bare file
	// name, so a same-file callee cannot bind to a same-named file of
	// another directory or another repository (#7642).
	uniqueNameByRepoPath map[string]map[string]map[string]string
	// uniqueNameByRepo maps a repository ID to the function/type names that
	// resolve to exactly one entity across the whole repository. A name absent
	// from the inner map was ambiguous repository-wide and must not be
	// resolved from it.
	uniqueNameByRepo    map[string]map[string]string
	uniqueNameByRepoDir map[string]map[string]map[string]string
	// constructorByRepoPath maps repositoryID -> file key -> class name ->
	// the constructor entity declared for that class in that file. The
	// file keying matches uniqueNameByRepoPath: the file's own keys only,
	// never a bare file name (#7642).
	constructorByRepoPath   map[string]map[string]map[string]string
	goMethodReturnTypes     map[string]map[string]string
	rustTraitMethodsByRepo  map[string]map[string]string
	pythonClassBasesByRepo  map[string]map[string][]string
	entityFileByID          map[string]string
	entityTypeByID          map[string]string
	entityByStableSymbolKey map[string]codeCallSymbolResolution
	// javaScriptAliasesByFile has the same repository and file keying as
	// spansByFile and holds the cached static-alias set of each JavaScript
	// function body.
	javaScriptAliasesByFile          map[string]map[string][]javaScriptStaticAliasSpan
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
// function name under one package import path. EntityID and RepositoryID are
// only safe to resolve when Count == 1; a count above one marks the name
// ambiguous across repositories and forces an unresolved result. The type
// itself stays unexported (obtained only through [EntityIndex.GoExportByImportPath]);
// its fields are exported so the golang leaf can read a returned value's fields.
type goCrossRepoExportEntry struct {
	EntityID     string
	RepositoryID string
	Count        int
}

// FunctionSpan records one function or type declaration's line range, entity
// id, and candidate names within a file, used to find the narrowest span that
// contains a reference line.
type FunctionSpan struct {
	StartLine int
	EndLine   int
	EntityID  string
	names     []string
}

// javaScriptStaticAliasSpan associates a cached JavaScriptAliasSet with the
// line range of the function source it was scanned from. The type stays
// unexported (obtained only through [EntityIndex.JavaScriptAliasesByFile]);
// its fields are exported so the javascript leaf can read a returned value's
// fields.
type javaScriptStaticAliasSpan struct {
	StartLine int
	EndLine   int
	Aliases   JavaScriptAliasSet
}

// EntityFileByID returns the preferred file path recorded for entityID, or ""
// when the entity is unknown.
func (idx EntityIndex) EntityFileByID(entityID string) string {
	return idx.entityFileByID[entityID]
}

// UniqueNameByRepoDir returns the entity id uniquely named candidateName
// within directory dir of repository repositoryID, or "" when no such unique
// declaration exists.
func (idx EntityIndex) UniqueNameByRepoDir(repositoryID, dir, candidateName string) string {
	return idx.uniqueNameByRepoDir[repositoryID][dir][candidateName]
}

// GoMethodReturnTypes returns the inferred return type for the
// "ReceiverType.MethodName" key within repository repositoryID, or "" when no
// unique return type was recorded.
func (idx EntityIndex) GoMethodReturnTypes(repositoryID, receiverMethodKey string) string {
	return idx.goMethodReturnTypes[repositoryID][receiverMethodKey]
}

// GoExportByImportPath returns the cross-repo export candidate recorded for
// name under Go package importPath, and whether one was recorded at all.
func (idx EntityIndex) GoExportByImportPath(importPath, name string) (goCrossRepoExportEntry, bool) {
	candidate, ok := idx.goExportByImportPath[importPath][name]
	return candidate, ok
}

// HasGoExports reports whether the index holds any Go cross-repo export
// entry, so the Go resolver can skip its per-call import walk when there is
// nothing to join against.
func (idx EntityIndex) HasGoExports() bool {
	return len(idx.goExportByImportPath) > 0
}

// JavaScriptAliasesByFile returns the cached static-alias spans recorded for
// the file whose own key (one of the [FileKeys] values) is fileKey within
// repositoryID, in ascending line order. A bare file name never matches.
func (idx EntityIndex) JavaScriptAliasesByFile(repositoryID, fileKey string) []javaScriptStaticAliasSpan {
	return idx.javaScriptAliasesByFile[repositoryID][fileKey]
}

// PythonClassBasesByRepo returns the declared base-class names for className
// within repository repositoryID, or nil when the class has no single
// unambiguous base list.
func (idx EntityIndex) PythonClassBasesByRepo(repositoryID, className string) []string {
	return idx.pythonClassBasesByRepo[repositoryID][className]
}

// RustTraitMethodsByRepo returns the entity id declaring the
// "TraitName::method" key within repository repositoryID, or "" when no
// unique declaration exists.
func (idx EntityIndex) RustTraitMethodsByRepo(repositoryID, traitMethodKey string) string {
	return idx.rustTraitMethodsByRepo[repositoryID][traitMethodKey]
}

// SpansByFile returns the function declaration spans recorded for the file
// whose own key (one of the [FileKeys] values) is fileKey within
// repositoryID, sorted by start line. A bare file name never matches.
func (idx EntityIndex) SpansByFile(repositoryID, fileKey string) []FunctionSpan {
	return idx.spansByFile[repositoryID][fileKey]
}

// TypeScriptInterfaceMethodsByRepo returns the entity id declaring methodName
// on interfaceName within repository repositoryID, or "" when no unique
// declaration exists.
func (idx EntityIndex) TypeScriptInterfaceMethodsByRepo(repositoryID, interfaceName, methodName string) string {
	return idx.typeScriptInterfaceMethodsByRepo[repositoryID][interfaceName][methodName]
}

// RepositoryImportPathsByRepo returns the cached, normalized, deduplicated
// import path set for repositoryID built by [CacheRepositoryImportPaths]. It
// exists for test fixtures in code/call that construct an EntityIndex
// directly; production resolution goes through
// [RepositoryImportPathsForResolution].
func (idx EntityIndex) RepositoryImportPathsByRepo(repositoryID string) []string {
	return idx.repositoryImportPathsByRepo[repositoryID]
}

// UniqueNameByPath returns the entity ID of the function or type named name
// when that name is unique within the file keyed by pathKey, or "" when the
// name is absent or ambiguous there. It is the read-only view of the per-path
// unique-name index BuildEntityIndex fills.
func (idx EntityIndex) UniqueNameByPath(pathKey, name string) string {
	return idx.uniqueNameByPath[pathKey][name]
}

// UniqueNameByRepoPath returns the entity ID of the function or type named
// name when that name is unique within the file whose own key (one of the
// [FileKeys] values) is fileKey in repository repositoryID, or "" when the
// name is absent or ambiguous there. A bare file name never matches, so a
// same-file lookup through this accessor cannot bind to a same-named file
// elsewhere (#7642).
func (idx EntityIndex) UniqueNameByRepoPath(repositoryID, fileKey, name string) string {
	return idx.uniqueNameByRepoPath[repositoryID][fileKey][name]
}

// UniqueNameByRepo returns the entity ID of the function or type named name
// when that name is unique within repositoryID, or "" when the name is absent
// or ambiguous there. It is the read-only view of the per-repository
// unique-name index BuildEntityIndex fills.
func (idx EntityIndex) UniqueNameByRepo(repositoryID, name string) string {
	return idx.uniqueNameByRepo[repositoryID][name]
}
