// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// goModuleRoot pins one Go module to the repository directory it is rooted in,
// so a file's package import path can be derived from its position under that
// root. relativeDir is the module-root directory inside the repository (empty
// for a repo-root module); modulePath is the declared go.mod module path.
type goModuleRoot struct {
	repositoryID string
	relativeDir  string
	modulePath   string
}

// buildGoCrossRepoExportIndex builds the durable cross-repo Go package-export
// index. It is accuracy-first: a name resolves only when exactly one exported
// top-level Go function exists for an import path across all repositories, and
// the import path is anchored on a defining module's declared module path. Zero
// or ambiguous candidates leave the name unresolvable.
//
// Identity is by import-path string, not module version: two ingested
// repositories that declare the same go.mod module path (a fork or mirror) are
// treated as one logical package. When both export the called name the result
// is ambiguous (count > 1) and stays unresolved; when only one defines it, that
// one resolves. This matches the per-repo resolver, which likewise has no
// go.sum/version pinning to distinguish forks.
func buildGoCrossRepoExportIndex(envelopes []facts.Envelope) map[string]map[string]goCrossRepoExportEntry {
	moduleRoots := collectGoModuleRoots(envelopes)
	if len(moduleRoots) == 0 {
		return nil
	}

	candidates := make(map[string]map[string]map[string]goCrossRepoExportEntry)
	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		preferredPath := codeCallPreferredPath(anyToString(fileData["path"]), payloadStr(env.Payload, "relative_path"))
		if !goSourceFile(fileData, preferredPath) {
			continue
		}
		importPath := goImportPathForFile(moduleRoots, repositoryID, preferredPath)
		if importPath == "" {
			continue
		}
		recordGoExportedFunctions(candidates, fileData, repositoryID, importPath)
	}

	return finalizeGoCrossRepoExportIndex(candidates)
}

// collectGoModuleRoots extracts the module-root directory and declared module
// path for every parsed go.mod fact, keyed per repository.
func collectGoModuleRoots(envelopes []facts.Envelope) []goModuleRoot {
	roots := make([]goModuleRoot, 0)
	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		modulePath := goModuleDeclaredPath(fileData)
		if modulePath == "" {
			continue
		}
		preferredPath := codeCallPreferredPath(anyToString(fileData["path"]), payloadStr(env.Payload, "relative_path"))
		roots = append(roots, goModuleRoot{
			repositoryID: repositoryID,
			relativeDir:  codeCallDirectoryKey(preferredPath),
			modulePath:   modulePath,
		})
	}
	return roots
}

// goModuleDeclaredPath returns the declared module path of a parsed go.mod fact,
// or empty when the fact is not a parsed Go module declaration.
func goModuleDeclaredPath(fileData map[string]any) string {
	if strings.TrimSpace(anyToString(fileData["lang"])) != "gomod" {
		return ""
	}
	if state, ok := fileData["gomod_state"].(map[string]any); ok {
		if modulePath := strings.TrimSpace(anyToString(state["module_path"])); modulePath != "" {
			return modulePath
		}
	}
	for _, row := range mapSlice(fileData["variables"]) {
		if strings.TrimSpace(anyToString(row["config_kind"])) != "module_declaration" {
			continue
		}
		if value := strings.TrimSpace(anyToString(row["value"])); value != "" {
			return value
		}
		if name := strings.TrimSpace(anyToString(row["name"])); name != "" {
			return name
		}
	}
	return ""
}

// goImportPathForFile resolves the Go package import path for a source file by
// matching it to the deepest module root in its own repository that contains
// the file. The deepest containing root wins so nested modules are honored.
func goImportPathForFile(moduleRoots []goModuleRoot, repositoryID, filePath string) string {
	fileDir := codeCallDirectoryKey(filePath)
	if fileDir == "" {
		return ""
	}
	best := ""
	bestRootLen := -1
	for _, root := range moduleRoots {
		if root.repositoryID != repositoryID {
			continue
		}
		rootDir := root.relativeDir
		if rootDir == "." {
			rootDir = ""
		}
		within, ok := goDirectoryWithin(rootDir, fileDir)
		if !ok {
			continue
		}
		if len(rootDir) <= bestRootLen {
			continue
		}
		best = goImportPathJoin(root.modulePath, within)
		bestRootLen = len(rootDir)
	}
	return best
}

// goDirectoryWithin reports whether fileDir is the module-root directory rootDir
// or a subdirectory of it, returning the path of fileDir relative to rootDir.
func goDirectoryWithin(rootDir, fileDir string) (string, bool) {
	rootDir = normalizeCodeCallPath(rootDir)
	fileDir = normalizeCodeCallPath(fileDir)
	if rootDir == "" {
		return fileDir, true
	}
	if fileDir == rootDir {
		return "", true
	}
	prefix := rootDir + "/"
	if strings.HasPrefix(fileDir, prefix) {
		return strings.TrimPrefix(fileDir, prefix), true
	}
	return "", false
}

// recordGoExportedFunctions tallies every exported top-level Go function in
// fileData under importPath. Methods (functions with a receiver or class
// context) and unexported names are excluded from the export surface.
func recordGoExportedFunctions(
	candidates map[string]map[string]map[string]goCrossRepoExportEntry,
	fileData map[string]any,
	repositoryID string,
	importPath string,
) {
	for _, item := range mapSlice(fileData["functions"]) {
		if strings.TrimSpace(anyToString(item["class_context"])) != "" {
			continue
		}
		if strings.TrimSpace(anyToString(item["receiver_type"])) != "" {
			continue
		}
		name := strings.TrimSpace(anyToString(item["name"]))
		if !goExportedName(name) {
			continue
		}
		entityID := strings.TrimSpace(anyToString(item["uid"]))
		if entityID == "" {
			continue
		}
		if _, ok := candidates[importPath]; !ok {
			candidates[importPath] = make(map[string]map[string]goCrossRepoExportEntry)
		}
		if _, ok := candidates[importPath][name]; !ok {
			candidates[importPath][name] = make(map[string]goCrossRepoExportEntry)
		}
		candidates[importPath][name][entityID] = goCrossRepoExportEntry{
			entityID:     entityID,
			repositoryID: repositoryID,
		}
	}
}

// finalizeGoCrossRepoExportIndex collapses the per-entity candidate tally into a
// resolvable index. A name with exactly one distinct entity across all
// repositories carries that entity; any name with more than one entity is kept
// with its count so the resolver can detect ambiguity and refuse to guess.
func finalizeGoCrossRepoExportIndex(
	candidates map[string]map[string]map[string]goCrossRepoExportEntry,
) map[string]map[string]goCrossRepoExportEntry {
	if len(candidates) == 0 {
		return nil
	}
	index := make(map[string]map[string]goCrossRepoExportEntry, len(candidates))
	for importPath, names := range candidates {
		index[importPath] = make(map[string]goCrossRepoExportEntry, len(names))
		for name, entities := range names {
			entry := goCrossRepoExportEntry{count: len(entities)}
			if len(entities) == 1 {
				for _, only := range entities {
					entry.entityID = only.entityID
					entry.repositoryID = only.repositoryID
				}
			}
			index[importPath][name] = entry
		}
	}
	return index
}

// resolveGoCrossRepoExportCalleeEntityID resolves a Go package-qualified call to
// an exported top-level function defined in another repository. It returns the
// callee entity id only when the import path joins to exactly one exported
// function and that function lives in a different repository than the caller.
func resolveGoCrossRepoExportCalleeEntityID(
	index codeEntityIndex,
	repositoryID string,
	fileData map[string]any,
	call map[string]any,
) string {
	if len(index.goExportByImportPath) == 0 || repositoryID == "" {
		return ""
	}
	fullName := strings.TrimSpace(anyToString(call["full_name"]))
	name := strings.TrimSpace(anyToString(call["name"]))
	qualifier, ok := goPackageQualifier(fullName, name)
	if !ok || !goExportedName(name) {
		return ""
	}
	for _, entry := range mapSlice(fileData["imports"]) {
		importSource := goImportSource(entry)
		if importSource == "" || goImportLocalName(entry, importSource) != qualifier {
			continue
		}
		importPath := normalizeCodeCallPath(importSource)
		candidate, found := index.goExportByImportPath[importPath][name]
		if !found || candidate.count != 1 || candidate.entityID == "" {
			continue
		}
		if candidate.repositoryID == repositoryID {
			continue
		}
		return candidate.entityID
	}
	return ""
}

// goExportedName reports whether a Go identifier is exported, i.e. its first
// rune is an uppercase letter. Unexported names are never part of the cross-repo
// export surface.
func goExportedName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	first := []rune(name)[0]
	return unicode.IsUpper(first)
}

// goSourceFile reports whether a parsed file fact is Go source (not a go.mod
// manifest), so only real package members feed the export index.
func goSourceFile(fileData map[string]any, filePath string) bool {
	if strings.TrimSpace(anyToString(fileData["lang"])) == "go" {
		return true
	}
	if strings.TrimSpace(anyToString(fileData["lang"])) != "" {
		return false
	}
	return strings.HasSuffix(strings.ToLower(normalizeCodeCallPath(filePath)), ".go")
}

// goImportPathJoin joins a module path and a relative directory into a Go
// package import path using slash semantics regardless of host separators.
func goImportPathJoin(modulePath, relativeDir string) string {
	relativeDir = normalizeCodeCallPath(relativeDir)
	if relativeDir == "" {
		return modulePath
	}
	return path.Join(modulePath, relativeDir)
}
