// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// codeCallPackageSymbolKeyPrefix starts the import-binding key the reducer
// derives from a definition's package_id and export_name
// (package:<package_id>#<export_name>).
const codeCallPackageSymbolKeyPrefix = "package:"

// listActiveCodeCallPackageManifestsQuery reads every stored package.json
// manifest of a repository scope that has an active generation. It reads every
// manifest except one under a path segment that begins with node_modules
// (case-insensitive, so node_modules.bak and Node_Modules-old count too).
// Nested workspace packages are included, because a package name maps to its
// repository wherever its manifest sits; the nearest-manifest rule applies when
// the parser stamps package_id on a definition, not here. A vendored copy of a
// dependency is not a publisher: counting it would anchor a scan of the whole
// backup repository for every consumer of that name and could mint false edges.
// Discovery already prunes the exact node_modules directory; this covers the
// renamed backups it does not.
//
// content_files holds the latest projected content of each repository and has
// no generation column. The definition scan that follows reads only active
// generation file facts and still matches each definition's own package_id, so
// a manifest that is ahead of or behind the active generation can only add or
// drop a candidate scope.
//
// The manifest read is a MATERIALIZED CTE on purpose. Inlined, the planner
// estimates the scope join at one row and probes content_files once per
// repository scope; on the ops-qa replica (2026-10-04) that took 125 ms warm
// and 758 ms cold over 155k buffers. Materialized, content_files is read once
// through content_files_relative_path_trgm_idx: 15 to 21 ms and 3,497 buffers
// for the same 469 rows. See
// docs/internal/evidence/7601-anchored-symbol-definition-loader.md.
const listActiveCodeCallPackageManifestsQuery = `
WITH manifest AS MATERIALIZED (
    SELECT repo_id, content
    FROM content_files
    WHERE (relative_path = 'package.json' OR relative_path LIKE '%/package.json')
      AND relative_path !~* '(^|/)node_modules[^/]*/'
)
SELECT
    scope.scope_id,
    manifest.content
FROM manifest
JOIN ingestion_scopes AS scope
  ON scope.source_key = manifest.repo_id
 AND scope.scope_kind = 'repository'
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
 AND generation.status = 'active'
`

// splitCodeCallPackageSymbolKeys separates package:<package_id>#<export_name>
// keys from every other symbol key, keeping the input order of each group.
func splitCodeCallPackageSymbolKeys(symbolKeys []string) (packageKeys, otherKeys []string) {
	for _, key := range symbolKeys {
		if strings.HasPrefix(key, codeCallPackageSymbolKeyPrefix) {
			packageKeys = append(packageKeys, key)
			continue
		}
		otherKeys = append(otherKeys, key)
	}
	return packageKeys, otherKeys
}

// codeCallPackageSymbolKeyPackageName returns the package_id of a
// package:<package_id>#<export_name> key, trimmed to match the trimmed manifest
// names, or "" when either part is empty.
// npm package names cannot contain '#', so the first '#' ends the name.
// Every key with the package: prefix takes the anchored path, so a key in any
// other shape (no '#', an empty part) resolves no producer and stays
// unresolved. No emitter produces such keys today; a new package: key shape
// must change this function too.
func codeCallPackageSymbolKeyPackageName(key string) string {
	packageName, exportName, ok := strings.Cut(strings.TrimPrefix(key, codeCallPackageSymbolKeyPrefix), "#")
	if !ok || strings.TrimSpace(packageName) == "" || strings.TrimSpace(exportName) == "" {
		return ""
	}
	return strings.TrimSpace(packageName)
}

// codeCallPackageManifestName returns the "name" of a package.json manifest.
// Invalid JSON, a non-object document, or a missing, blank, or non-string
// name yields "" so one bad manifest never fails the load.
func codeCallPackageManifestName(content string) string {
	var manifest struct {
		Name any `json:"name"`
	}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return ""
	}
	name, _ := manifest.Name.(string)
	return strings.TrimSpace(name)
}

// listCodeCallPackageProducerScopeIDs resolves the package keys to the sorted,
// distinct scope ids whose stored manifests publish one of the named packages.
// A package published by several repositories returns every one of them, so
// the reducer sees each candidate definition and keeps the key unresolved.
func (s FactStore) listCodeCallPackageProducerScopeIDs(
	ctx context.Context,
	packageKeys []string,
) ([]string, error) {
	packageNames := make(map[string]struct{}, len(packageKeys))
	for _, key := range packageKeys {
		if name := codeCallPackageSymbolKeyPackageName(key); name != "" {
			packageNames[name] = struct{}{}
		}
	}
	if len(packageNames) == 0 {
		return nil, nil
	}

	rows, err := s.database.QueryContext(ctx, listActiveCodeCallPackageManifestsQuery)
	if err != nil {
		return nil, fmt.Errorf("list code call package producer manifests: %w", err)
	}
	defer func() { _ = rows.Close() }()

	scopeIDs := make([]string, 0)
	for rows.Next() {
		var scopeID, content string
		if err := rows.Scan(&scopeID, &content); err != nil {
			return nil, fmt.Errorf("list code call package producer manifests: %w", err)
		}
		if _, ok := packageNames[codeCallPackageManifestName(content)]; ok {
			scopeIDs = append(scopeIDs, scopeID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list code call package producer manifests: %w", err)
	}

	slices.Sort(scopeIDs)
	return slices.Compact(scopeIDs), nil
}

// codeCallGoSymbolKeyPrefix starts the Go stable symbol key the parser derives
// from a package-qualified imported call (`scip-go gomod <import_path> ...`).
const codeCallGoSymbolKeyPrefix = "scip-go gomod "

// listActiveCodeCallGoModuleManifestsQuery reads every stored go.mod manifest
// of a repository scope that has an active generation. It mirrors the
// package.json manifest read, including the MATERIALIZED CTE: inlined, the
// planner probes content_files once per repository scope. Unlike the
// package.json read it excludes no path: discovery already prunes vendor/
// trees, and the reducer's Go module index honors every remaining module
// root, so every stored go.mod is a candidate producer.
const listActiveCodeCallGoModuleManifestsQuery = `
WITH manifest AS MATERIALIZED (
    SELECT repo_id, content
    FROM content_files
    WHERE (relative_path = 'go.mod' OR relative_path LIKE '%/go.mod')
)
SELECT
    scope.scope_id,
    manifest.content
FROM manifest
JOIN ingestion_scopes AS scope
  ON scope.source_key = manifest.repo_id
 AND scope.scope_kind = 'repository'
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
 AND generation.status = 'active'
`

// splitCodeCallGoSymbolKeys separates `scip-go gomod <import_path> ...` keys
// from every other symbol key, keeping the input order of each group. A key
// with the prefix but no import path is not a Go key: it keeps the
// corpus-wide scan.
func splitCodeCallGoSymbolKeys(symbolKeys []string) (goKeys, otherKeys []string) {
	for _, key := range symbolKeys {
		if codeCallGoSymbolImportPath(key) == "" {
			otherKeys = append(otherKeys, key)
			continue
		}
		goKeys = append(goKeys, key)
	}
	return goKeys, otherKeys
}

// codeCallGoSymbolImportPath returns the package import path carried by a
// `scip-go gomod <import_path> ...` key, or "" when the key has another
// shape. The import path is the first whitespace-separated field after the
// prefix.
func codeCallGoSymbolImportPath(key string) string {
	rest, ok := strings.CutPrefix(key, codeCallGoSymbolKeyPrefix)
	if !ok {
		return ""
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// codeCallGoModuleManifestPath returns the module path declared by a go.mod
// manifest's `module` directive, or "" when the content has none. It scans
// past blank lines and `//` comments and takes the second field of the first
// `module` line, so a trailing comment never leaks into the path. Carriage
// returns fold to newlines first so a bare-CR manifest parses the same way
// the Go parser's line-ending normalization reads it. One bad manifest
// never fails the load.
func codeCallGoModuleManifestPath(content string) string {
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r", "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "module" {
			continue
		}
		if path, _, _ := strings.Cut(fields[1], "//"); path != "" {
			return path
		}
	}
	return ""
}

// codeCallGoImportPathInModule reports whether importPath names the module
// itself or a package inside it: either it equals the module path or it
// continues past it at a path boundary. A shared string prefix without the
// boundary (github.com/acme/libext against github.com/acme/lib) does not
// match.
func codeCallGoImportPathInModule(importPath, module string) bool {
	if importPath == "" || module == "" {
		return false
	}
	return importPath == module || strings.HasPrefix(importPath, module+"/")
}

// listCodeCallGoModuleProducerScopeIDs resolves the Go keys to the sorted,
// distinct scope ids whose stored go.mod manifests declare a module that is a
// prefix of one of the keys' import paths. A module declared by several
// repositories returns every one of them, so the reducer sees each candidate
// definition and keeps the key unresolved.
func (s FactStore) listCodeCallGoModuleProducerScopeIDs(
	ctx context.Context,
	goKeys []string,
) ([]string, error) {
	importPaths := make([]string, 0, len(goKeys))
	for _, key := range goKeys {
		if importPath := codeCallGoSymbolImportPath(key); importPath != "" {
			importPaths = append(importPaths, importPath)
		}
	}
	if len(importPaths) == 0 {
		return nil, nil
	}

	rows, err := s.database.QueryContext(ctx, listActiveCodeCallGoModuleManifestsQuery)
	if err != nil {
		return nil, fmt.Errorf("list code call go module producer manifests: %w", err)
	}
	defer func() { _ = rows.Close() }()

	scopeIDs := make([]string, 0)
	for rows.Next() {
		var scopeID, content string
		if err := rows.Scan(&scopeID, &content); err != nil {
			return nil, fmt.Errorf("list code call go module producer manifests: %w", err)
		}
		module := codeCallGoModuleManifestPath(content)
		if module == "" {
			continue
		}
		for _, importPath := range importPaths {
			if codeCallGoImportPathInModule(importPath, module) {
				scopeIDs = append(scopeIDs, scopeID)
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list code call go module producer manifests: %w", err)
	}

	slices.Sort(scopeIDs)
	return slices.Compact(scopeIDs), nil
}
