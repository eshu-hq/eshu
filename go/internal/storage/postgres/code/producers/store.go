// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
// content_files holds the latest projected content of each repository, stamped
// with the writing generation (#7760). A stored manifest can be ahead of the
// active generation: written by a generation that never activated (#7609).
// The active generation's manifest is then unrecoverable from content_files,
// so each manifest resolves by its tag: content is used only when the tag's
// generation row exists with activated_at set. Any other tag state (NULL
// legacy, dangling or retention-pruned, empty, or never-activated) fails safe
// to a dirty NULL row. Per-row tags cannot see row-less scopes, so the
// manifest-less leg keeps the never-activated-generation signal for repository
// scopes with no stored manifest. Extra scopes only add candidates the anchored
// scan still gates on each definition's own package_id, so dirty rows keep the
// ambiguity rule exact while a dropped scope would bypass it. UNION ALL is safe:
// the consumer sorts and compacts scope ids in Go, and NULL dirty rows can never
// equal non-NULL manifest rows (content is NOT NULL).
//
// The tag rule is status-agnostic on purpose: Ack supersedes a refused
// generation at the next activation, and a delta that does not touch package.json
// leaves the refused content stored, so the tag's activated_at (never the status)
// decides. A tag from an older activated generation is clean by carry-forward:
// the active generation never rewrote the path, so the stored bytes are still
// the active truth. Do NOT compare superseded_at to activated_at: Ack stamps
// the prior active with the same clock, which would dirty every scope with two
// activations.
//
// Residual: a manifest deleted by a write with no generation row leaves no row
// and no tag, and is uncatchable; accepted as second-order (requires a
// crash-window write plus an ambiguity-relevant load). Second residual: the
// migration-169 backfill attributes rows by indexed_at against the activation,
// so a behind-skewed writer clock on a post-retention-hole scope can wrong-clean
// exactly as the timestamp leg this tag replaces could; live unactivated
// generations are still caught by the tag regardless of skew.
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
    SELECT repo_id, content, generation_id
    FROM content_files
    WHERE (relative_path = 'package.json' OR relative_path LIKE '%/package.json')
      AND relative_path !~* '(^|/)node_modules[^/]*/'
),
tagged AS (
    SELECT manifest.repo_id, manifest.content, manifest.generation_id AS tag,
           tag.generation_id AS tag_row_id, tag.activated_at AS tag_activated_at
    FROM manifest
    LEFT JOIN scope_generations AS tag ON tag.generation_id = manifest.generation_id
)
SELECT
    scope.scope_id,
    CASE WHEN tagged.tag_activated_at IS NOT NULL THEN tagged.content END,
    CASE WHEN tagged.tag_activated_at IS NOT NULL THEN 'clean'
         WHEN tagged.tag IS NULL THEN 'null_tag'
         WHEN tagged.tag_row_id IS NULL THEN 'dangling_tag'
         ELSE 'unactivated_tag' END
FROM tagged
JOIN ingestion_scopes AS scope
  ON scope.source_key = tagged.repo_id
 AND scope.scope_kind = 'repository'
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
 AND generation.status = 'active'
UNION ALL
SELECT scope.scope_id, NULL, 'manifest_less'
FROM ingestion_scopes AS scope
JOIN scope_generations AS active
  ON active.scope_id = scope.scope_id
 AND active.generation_id = scope.active_generation_id
 AND active.status = 'active'
WHERE scope.scope_kind = 'repository'
  AND NOT EXISTS (
    SELECT 1
    FROM manifest
    WHERE manifest.repo_id = scope.source_key
  )
  AND EXISTS (
    SELECT 1
    FROM scope_generations AS pending
    WHERE pending.scope_id = scope.scope_id
      AND pending.activated_at IS NULL
  )
`

// GoModuleManifestsQuery reads every stored go.mod manifest
// of a repository scope that has an active generation. It mirrors the
// package.json manifest read, including the MATERIALIZED CTE and the
// generation-tag rule (#7760): content resolves only when the writing
// generation activated, and any other tag state fails safe to a dirty NULL
// row the anchored scan always visits. Unlike the package.json read it
// excludes no path: discovery already prunes vendor/ trees, and the
// reducer's Go module index honors every remaining module root, so every
// stored go.mod is a candidate producer. Behavior change versus the blind
// predecessor: ahead go.mod content no longer resolves alone.
const GoModuleManifestsQuery = `
WITH manifest AS MATERIALIZED (
    SELECT repo_id, content, generation_id
    FROM content_files
    WHERE (relative_path = 'go.mod' OR relative_path LIKE '%/go.mod')
),
tagged AS (
    SELECT manifest.repo_id, manifest.content, manifest.generation_id AS tag,
           tag.generation_id AS tag_row_id, tag.activated_at AS tag_activated_at
    FROM manifest
    LEFT JOIN scope_generations AS tag ON tag.generation_id = manifest.generation_id
)
SELECT
    scope.scope_id,
    CASE WHEN tagged.tag_activated_at IS NOT NULL THEN tagged.content END,
    CASE WHEN tagged.tag_activated_at IS NOT NULL THEN 'clean'
         WHEN tagged.tag IS NULL THEN 'null_tag'
         WHEN tagged.tag_row_id IS NULL THEN 'dangling_tag'
         ELSE 'unactivated_tag' END
FROM tagged
JOIN ingestion_scopes AS scope
  ON scope.source_key = tagged.repo_id
 AND scope.scope_kind = 'repository'
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
 AND generation.status = 'active'
UNION ALL
SELECT scope.scope_id, NULL, 'manifest_less'
FROM ingestion_scopes AS scope
JOIN scope_generations AS active
  ON active.scope_id = scope.scope_id
 AND active.generation_id = scope.active_generation_id
 AND active.status = 'active'
WHERE scope.scope_kind = 'repository'
  AND NOT EXISTS (
    SELECT 1
    FROM manifest
    WHERE manifest.repo_id = scope.source_key
  )
  AND EXISTS (
    SELECT 1
    FROM scope_generations AS pending
    WHERE pending.scope_id = scope.scope_id
      AND pending.activated_at IS NULL
  )
`

// Store reads the producer manifests over an injected database handle.
type Store struct {
	database    db.Queryer
	instruments *telemetry.Instruments
}

// New returns a Store over database.
func New(database db.Queryer) Store {
	return Store{database: database}
}

// WithInstruments returns a copy that records manifest tag outcomes.
func (s Store) WithInstruments(instruments *telemetry.Instruments) Store {
	s.instruments = instruments
	return s
}

// recordTagOutcomes adds one counter sample per tag outcome observed in a
// manifest read. It no-ops without instruments; the kind label is the closed
// pair the two manifest reads pass (package|gomod), never the error text.
func (s Store) recordTagOutcomes(ctx context.Context, kind string, outcomes map[string]int64) {
	if s.instruments == nil {
		return
	}
	metricKind := "package"
	if kind != "package" {
		metricKind = "gomod"
	}
	for outcome, count := range outcomes {
		s.instruments.ProducerManifestTagOutcomes.Add(ctx, count, metric.WithAttributes(
			telemetry.AttrKind(metricKind),
			telemetry.AttrOutcome(outcome),
		))
	}
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
// scope (#7609, #7760) and is always included: its tag is unknown, dangling,
// or never-activated, so the anchored scan must visit it. Each row's tag
// outcome is counted into ProducerManifestTagOutcomes once per read when the
// store carries instruments.
func (s Store) scopeIDsWhere(ctx context.Context, query, kind string, match func(content string) bool) ([]string, error) {
	rows, err := s.database.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list code call %s producer manifests: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()

	outcomes := make(map[string]int64)
	scopeIDs := make([]string, 0)
	for rows.Next() {
		var scopeID string
		var content sql.NullString
		var outcome string
		if err := rows.Scan(&scopeID, &content, &outcome); err != nil {
			return nil, fmt.Errorf("list code call %s producer manifests: %w", kind, err)
		}
		outcomes[outcome]++
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
	s.recordTagOutcomes(ctx, kind, outcomes)

	slices.Sort(scopeIDs)
	return slices.Compact(scopeIDs), nil
}
