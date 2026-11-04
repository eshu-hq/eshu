// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const listActivePackageManifestDependencyFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'content_entity'
  AND fact.source_system = 'git'
  AND fact.payload->>'entity_type' = 'Variable'
  AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
  AND fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])
  AND (LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") = ANY($2::text[])
  AND generation.status = 'active'
  AND fact.is_tombstone = FALSE
  AND (
    $3::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $5
`

// ListActivePackageManifestDependencyFacts loads active, non-tombstoned Git
// manifest dependency entities for the specific ecosystem/name set requested by
// a package-registry reducer intent.
//
// A scope whose active generation is a git delta reads its delta overlaid on
// its #7797 base full generation (#9991): base rows keep their own generation
// id, and the delta wins per stable key, by tombstone, and by touched path.
// A refused, legacy, or mismatched base keeps the delta-only rows; see
// overlayDeltaManifestBase. Every statement runs on one read-only
// repeatable-read snapshot.
// Performance Evidence: docs/internal/evidence/9991-manifest-read-delta-union.md.
//
// The query filters fact.is_tombstone = FALSE so a manifest dependency fact
// tombstoned within a still-active generation is never surfaced as live. The
// active generation pointer keeps tombstoned rows joinable, so the predicate is
// the only guard against returning superseded dependency entities to
// correlation consumers; the package-source and security-alert reconciliation
// handlers do not filter IsTombstone themselves. This matches every sibling
// active source-local reader.
//
// No-Regression Evidence: predicate-only narrowing of an existing WHERE clause
// on the active-generation read. The partial
// fact_records_active_package_dependency_entity_idx index does not cover
// is_tombstone, so the planner applies the new predicate as a residual filter
// on rows the index scan already visits; the index choice, the
// active-generation join, the keyset pagination cursor, ordering, and the scan
// shape are unchanged.
// go test ./internal/storage/postgres ./internal/reducer -count=1.
//
// Case-Fold Evidence (#9568): the entity_name prefilter folds the stored name
// with LOWER(TRIM(entity_name) COLLATE "C"). The LOWER under COLLATE "C" is
// byte-identical to Go packageidentity.LowerASCII; TRIM strips spaces, which
// matches how the $2 candidate set is trimmed for real parser output (Go
// strings.TrimSpace would also strip other unicode whitespace, which parsers
// never emit in a name). Manifests store registry case (NuGet Newtonsoft.Json,
// pypi PyYAML). The $2 candidate set folds the same way, and the key join in
// packageidentity.ManifestConsumptionKeys plus the join/cursor/ordering shape
// are unchanged, so the key join stays the accuracy gate.
// Migration 247 adds the mirrored
// fact_records_active_package_dependency_folded_entity_idx expression index;
// on a disposable PostgreSQL 18.6 fixture the folded read uses an index scan
// with no sequential scan in every measured shape, and a selective name seeks
// the new index in 1.2-1.7 ms versus 16-21 ms through the old prefix.
// No-Observability-Change: the existing read_seconds/read_pages metrics cover
// this read; the impact handler's manifest_dependency_facts count goes from 0
// to the matched row count for mixed-case consumers.
// Observability Evidence: each completed read records its wall time and page
// count in eshu_dp_package_manifest_dependency_read_seconds and
// eshu_dp_package_manifest_dependency_read_pages, labeled read (#9586).
func (s FactStore) ListActivePackageManifestDependencyFacts(
	ctx context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, error) {
	started := time.Now()
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	if len(ecosystems) == 0 || len(packageNames) == 0 {
		return nil, nil
	}

	packageManagers, query := activePackageManifestDependencyQuery(ecosystems)
	// The read label names the query text that ran: keep the pre-transform
	// text, because on a snapshot clone the static rewrite below changes
	// the text and no rewritten text is in the label table.
	rawQuery := query
	baseQuery, err := deltaBasePackageManifestDependencyQuery(query)
	if err != nil {
		return nil, err
	}
	if query, err = s.protectedImpactStaticFactSQL(query); err != nil {
		return nil, err
	}
	if baseQuery, err = s.protectedImpactStaticFactSQL(baseQuery); err != nil {
		return nil, err
	}
	// One read-only repeatable-read snapshot keeps the active pages, the
	// delta-base pages, and the base resolution on the same active
	// generations (#9991). Inside the impact snapshot this nests as a no-op.
	var loaded []facts.Envelope
	activeRows := 0
	err = withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		reader := s
		reader.database = snapshotReadQueryer{Queryer: queryer}
		active, err := reader.listPackageManifestDependencyFactPages(ctx, query, packageManagers, packageNames)
		if err != nil {
			return err
		}
		activeRows = len(active)
		read := manifestDependencyReadLabel(rawQuery)
		targets, err := reader.readManifestDeltaTargets(ctx)
		if err != nil {
			reader.recordManifestDeltaUnionFailure(ctx, read)
			return err
		}
		var base []facts.Envelope
		if baseScopes := manifestDeltaBaseScopes(targets); len(baseScopes) > 0 {
			base, err = reader.listPackageManifestDependencyFactPages(ctx, baseQuery, packageManagers, packageNames, baseScopes)
			if err != nil {
				reader.recordManifestDeltaUnionFailure(ctx, read)
				return err
			}
		}
		loaded, err = reader.overlayDeltaManifestBase(ctx, read, targets, active, base)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.recordManifestDependencyRead(ctx, rawQuery, started, activeRows)
	return loaded, nil
}

// listPackageManifestDependencyFactPages runs one manifest dependency query
// text through every keyset page. extra binds as $6 onward on every page.
func (s FactStore) listPackageManifestDependencyFactPages(
	ctx context.Context,
	query string,
	packageManagers []string,
	packageNames []string,
	extra ...any,
) ([]facts.Envelope, error) {
	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActivePackageManifestDependencyFactsPage(
			ctx,
			query,
			packageManagers,
			packageNames,
			cursorObservedAt,
			cursorFactID,
			extra...,
		)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}

		last := page[len(page)-1]
		observedAt := last.ObservedAt.UTC()
		cursorObservedAt = &observedAt
		cursorFactID = last.FactID
	}
}

func (s FactStore) listActivePackageManifestDependencyFactsPage(
	ctx context.Context,
	query string,
	packageManagers []string,
	packageNames []string,
	cursorObservedAt *time.Time,
	cursorFactID string,
	extra ...any,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	args := append([]any{
		manifestDependencyQueryExecMode,
		packageManagers,
		packageNames,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	}, extra...)
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active package manifest dependency facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active package manifest dependency facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active package manifest dependency facts: %w", err)
	}
	return loaded, nil
}

// listActivePackageManifestDependencyFactsWithSBTQuery is the Maven-family
// read (#8961). Its first branch is the existing query with package_manager
// widened to the Maven family, so Gradle and sbt `%` rows (whose name is the
// exact coordinate) join through entity_name. Its second branch reaches sbt
// `%%` rows, whose entity_name is the unsuffixed organization:name and whose
// published artifact rides in sbt_binary_artifact. The branches are disjoint
// (the second excludes entity_name values the first already matches), each is
// keyset-bounded and limited on its own, and the outer sort restores the one
// ordering and page size the cursor depends on.
//
// Both branches over-select by design: the reducer's
// packageidentity.ManifestConsumptionKeys is the accuracy gate, so a row this
// query admits but the helper rejects (a `%%%` row, say) joins nothing. Both
// branches fold entity_name with LOWER(TRIM(entity_name) COLLATE "C") (#9568):
// branch 1 so mixed-case manifest rows load, branch 2's exclusion so it stays
// disjoint from branch 1 (an exact name in $2 is also a folded name in $2).
// The sbt_binary_artifact comparison stays exact; artifacts ride the parser's
// case the same way the Maven coordinate does (#9286).
//
// Performance Evidence (throwaway Postgres 18, 1.3M git dependency rows plus
// 0.4M other content_entity rows, 13,000 sbt rows, 4,000 active scopes, 400
// names in $2, LIMIT 5000, cursor NULL then set; psql EXPLAIN (ANALYZE,
// BUFFERS) before and after): the sbt branch scans only the package_manager
// = 'sbt' prefix of fact_records_active_package_dependency_entity_idx (13,000
// entries, about 14,000 buffers, no sequential scan, no new index). With the
// planner left alone both shapes took the same scope-driven nested loop the
// current query already takes on this synthetic data: 3,644 ms before and
// 6,599 ms after, because widening package_manager from one value to three
// roughly doubles the index searches (1.47M to 2.90M) and reaches 1,949 rows
// instead of 1,117. With nested loops disabled the same query is 5.1 ms
// (about 2,300 buffers) before and
// 29.2 ms (about 17,400 buffers) after; with the cursor set 8.9 ms after.
// The cost the sbt branch adds is linear in the number of sbt dependency
// rows, not in the corpus.
//
// No-Observability-Change: same FactStore read path, instrumentation, and
// row shape as listActivePackageManifestDependencyFactsQuery.
const listActivePackageManifestDependencyFactsWithSBTQuery = `
SELECT
    u.fact_id, u.scope_id, u.generation_id, u.fact_kind, u.stable_fact_key,
    u.schema_version, u.collector_kind, u.fencing_token, u.source_confidence,
    u.source_system, u.source_fact_key, u.source_uri, u.source_record_id,
    u.observed_at, u.is_tombstone, u.payload
FROM (
  (SELECT
      fact.fact_id,
      fact.scope_id,
      fact.generation_id,
      fact.fact_kind,
      fact.stable_fact_key,
      fact.schema_version,
      fact.collector_kind,
      fact.fencing_token,
      fact.source_confidence,
      fact.source_system,
      fact.source_fact_key,
      COALESCE(fact.source_uri, '') AS source_uri,
      COALESCE(fact.source_record_id, '') AS source_record_id,
      fact.observed_at,
      fact.is_tombstone,
      fact.payload
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = 'content_entity'
    AND fact.source_system = 'git'
    AND fact.payload->>'entity_type' = 'Variable'
    AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
    AND fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])
    AND (LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") = ANY($2::text[])
    AND generation.status = 'active'
    AND fact.is_tombstone = FALSE
    AND (
      $3::timestamptz IS NULL
      OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
    )
  ORDER BY fact.observed_at ASC, fact.fact_id ASC
  LIMIT $5)
  UNION ALL
  (SELECT
      fact.fact_id,
      fact.scope_id,
      fact.generation_id,
      fact.fact_kind,
      fact.stable_fact_key,
      fact.schema_version,
      fact.collector_kind,
      fact.fencing_token,
      fact.source_confidence,
      fact.source_system,
      fact.source_fact_key,
      COALESCE(fact.source_uri, '') AS source_uri,
      COALESCE(fact.source_record_id, '') AS source_record_id,
      fact.observed_at,
      fact.is_tombstone,
      fact.payload
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = 'content_entity'
    AND fact.source_system = 'git'
    AND fact.payload->>'entity_type' = 'Variable'
    AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
    AND fact.payload->'entity_metadata'->>'package_manager' = 'sbt'
    AND fact.payload->'entity_metadata'->>'sbt_binary_artifact' = ANY($2::text[])
    AND (LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") <> ALL($2::text[])
    AND generation.status = 'active'
    AND fact.is_tombstone = FALSE
    AND (
      $3::timestamptz IS NULL
      OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
    )
  ORDER BY fact.observed_at ASC, fact.fact_id ASC
  LIMIT $5)
) AS u
ORDER BY u.observed_at ASC, u.fact_id ASC
LIMIT $5
`

// activePackageManifestDependencyQuery returns the package_manager values and
// the query one ListActivePackageManifestDependencyFacts call uses. Each
// ecosystem expands to the parser package_manager values it can represent.
// Maven selects the sbt-aware query; other ecosystems use the original query.
func activePackageManifestDependencyQuery(ecosystems []string) ([]string, string) {
	hasMaven := slices.ContainsFunc(ecosystems, func(ecosystem string) bool {
		return packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem)) == packageidentity.EcosystemMaven
	})
	expanded := make([]string, 0, len(ecosystems)+2)
	for _, ecosystem := range ecosystems {
		expanded = append(expanded, packageidentity.ManifestPackageManagersForEcosystem(ecosystem)...)
	}
	slices.Sort(expanded)
	expanded = slices.Compact(expanded)
	if !hasMaven {
		return expanded, listActivePackageManifestDependencyFactsQuery
	}
	return expanded, listActivePackageManifestDependencyFactsWithSBTQuery
}
