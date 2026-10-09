// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"context"
	"database/sql"
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
// no generation column, so a stored manifest can be ahead of the active
// generation: written by a generation that never activated (#7609). The
// active generation's manifest is then unrecoverable from content_files, so
// the producer set is the manifest match UNION ALL the dirty scopes: a scope
// with a never-activated generation, an unstamped activation, or a manifest
// newer than its activation. Extra scopes only add candidates the anchored scan
// still gates on each definition's own package_id, so the union keeps the
// ambiguity rule exact while a dropped scope would bypass it. UNION ALL is safe:
// the consumer sorts and compacts scope ids in Go, and NULL dirty rows can never
// equal non-NULL manifest rows (content is NOT NULL).
//
// The dirty predicate is status-agnostic on purpose: Ack supersedes a refused
// generation at the next activation, and a delta that does not touch package.json
// leaves the refused content stored, so "pending/failed" or "newer than active"
// both miss the supersede-then-delta hole. Generations outside the delta-baseline
// chain are exactly the never-activated ones. Do NOT compare superseded_at to
// activated_at: Ack stamps the prior active with the same clock, which would dirty
// every scope with two activations.
//
// Both dirty legs are joins, not correlated EXISTS: the timestamp leg reads a
// per-repository MAX(indexed_at) aggregate (MAX >= t is exactly EXISTS >= t) and
// the generation leg is an IN semi-join the planner hashes once. manifest_max MUST
// stay a LEFT JOIN so manifest-less scopes still evaluate the generation leg. The IN
// list includes the active row itself when unstamped; the first disjunct covers it.
//
// Bounds on the timestamp leg: indexed_at and activated_at are app clocks
// (ContentWriter.now / ProjectorQueue.now) that may live on different hosts,
// so skew beyond the inter-generation gap can false-negative; a missing
// activated_at fails safe to dirty. Residual: a manifest deleted by a write
// with no generation row leaves no indexed_at trace and is uncatchable;
// accepted as second-order (requires a crash-window write plus an
// ambiguity-relevant load). Third residual: retention prunes superseded
// never-activated generation rows regardless of activated_at
// (generation_retention_sql.go), so once the signal row ages out, a post-hole
// scope (stale manifest; deltas never rewrite the untouched path) goes clean
// on both legs and returns to RED. The window opens only past the retention
// horizon with an ambiguity-relevant load inside it. The permanent fix is the
// generation tag on content_files (#7760).
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
    SELECT repo_id, content, indexed_at
    FROM content_files
    WHERE (relative_path = 'package.json' OR relative_path LIKE '%/package.json')
      AND relative_path !~* '(^|/)node_modules[^/]*/'
),
manifest_max AS (
    SELECT repo_id, MAX(indexed_at) AS max_indexed_at FROM manifest GROUP BY repo_id
),
dirty AS (
    SELECT scope.scope_id
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS active
      ON active.scope_id = scope.scope_id
     AND active.generation_id = scope.active_generation_id
    LEFT JOIN manifest_max
      ON manifest_max.repo_id = scope.source_key
    WHERE scope.scope_kind = 'repository'
      AND active.status = 'active'
      AND (
        active.activated_at IS NULL
        OR scope.scope_id IN (SELECT scope_id FROM scope_generations WHERE activated_at IS NULL)
        OR manifest_max.max_indexed_at >= active.activated_at
      )
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
UNION ALL
SELECT dirty.scope_id, NULL
FROM dirty
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
// packages, plus every dirty scope. A package published by several repositories
// returns every one of them, so the reducer sees each candidate definition and
// keeps the key unresolved. A NULL manifest marks a dirty scope (#7609): its
// stored content may be ahead of its active generation, so it is always scanned
// and the anchored definition match still gates on each definition's own
// package_id.
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
// ids whose manifest content satisfies match. A NULL manifest marks a dirty
// scope (#7609) and is always included; only the package read returns NULL
// rows (content_files.content is NOT NULL, so the go.mod read never does).
func (s Store) scopeIDsWhere(ctx context.Context, query, kind string, match func(content string) bool) ([]string, error) {
	rows, err := s.database.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list code call %s producer manifests: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()

	scopeIDs := make([]string, 0)
	for rows.Next() {
		var scopeID string
		var content sql.NullString
		if err := rows.Scan(&scopeID, &content); err != nil {
			return nil, fmt.Errorf("list code call %s producer manifests: %w", kind, err)
		}
		if !content.Valid {
			scopeIDs = append(scopeIDs, scopeID)
			continue
		}
		if match(content.String) {
			scopeIDs = append(scopeIDs, scopeID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list code call %s producer manifests: %w", kind, err)
	}

	slices.Sort(scopeIDs)
	return slices.Compact(scopeIDs), nil
}
