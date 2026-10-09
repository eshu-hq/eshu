// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"context"
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// PackageManifestsQuery reads every stored package.json
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
const PackageManifestsQuery = `
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

// GoModuleManifestsQuery reads every stored go.mod manifest
// of a repository scope that has an active generation. It mirrors the
// package.json manifest read, including the MATERIALIZED CTE: inlined, the
// planner probes content_files once per repository scope. Unlike the
// package.json read it excludes no path: discovery already prunes vendor/
// trees, and the reducer's Go module index honors every remaining module
// root, so every stored go.mod is a candidate producer.
const GoModuleManifestsQuery = `
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

// Store reads the producer manifests over an injected database handle.
type Store struct {
	database db.Queryer
}

// New returns a Store over database.
func New(database db.Queryer) Store {
	return Store{database: database}
}

// PackageScopeIDs resolves package:<id>#<export> keys to the sorted, distinct
// scope ids whose stored package.json manifests publish one of the named
// packages. A package published by several repositories returns every one of
// them, so the reducer sees each candidate definition and keeps the key
// unresolved.
func (s Store) PackageScopeIDs(ctx context.Context, packageKeys []string) ([]string, error) {
	packageNames := make(map[string]struct{}, len(packageKeys))
	for _, key := range packageKeys {
		if name := PackageName(key); name != "" {
			packageNames[name] = struct{}{}
		}
	}
	if len(packageNames) == 0 {
		return nil, nil
	}
	return s.scopeIDsWhere(ctx, PackageManifestsQuery, "package", func(content string) bool {
		_, ok := packageNames[PackageManifestName(content)]
		return ok
	})
}

// GoModuleScopeIDs resolves scip-go gomod keys to the sorted, distinct scope
// ids whose stored go.mod module path equals the import path of a key or one of
// its '/'-prefixes. A module declared by several repositories returns every one
// of them, so the reducer keeps the key unresolved. A key whose module no
// repository declares (the standard library, a dependency outside the corpus)
// resolves nothing, and the caller issues no definition scan for it.
func (s Store) GoModuleScopeIDs(ctx context.Context, goKeys []string) ([]string, error) {
	modulePaths := make(map[string]struct{}, len(goKeys))
	for _, key := range goKeys {
		for _, candidate := range GoModuleCandidates(GoImportPath(key)) {
			modulePaths[candidate] = struct{}{}
		}
	}
	if len(modulePaths) == 0 {
		return nil, nil
	}
	return s.scopeIDsWhere(ctx, GoModuleManifestsQuery, "go module", func(content string) bool {
		_, ok := modulePaths[GoModuleName(content)]
		return ok
	})
}

// scopeIDsWhere runs one manifest read and returns the sorted, distinct scope
// ids whose manifest content satisfies match.
func (s Store) scopeIDsWhere(ctx context.Context, query, kind string, match func(content string) bool) ([]string, error) {
	rows, err := s.database.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list code call %s producer manifests: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()

	scopeIDs := make([]string, 0)
	for rows.Next() {
		var scopeID, content string
		if err := rows.Scan(&scopeID, &content); err != nil {
			return nil, fmt.Errorf("list code call %s producer manifests: %w", kind, err)
		}
		if match(content) {
			scopeIDs = append(scopeIDs, scopeID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list code call %s producer manifests: %w", kind, err)
	}

	slices.Sort(scopeIDs)
	return slices.Compact(scopeIDs), nil
}
