// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	defaultOwnedPackageDependencyTargetLimit = 100
	maxOwnedPackageDependencyTargetLimit     = 5000
)

// ownedPackageEcosystemExpression returns the SQL expression that names a
// dependency fact's ecosystem. Without alias expansion it is the lowercased raw
// package_manager. With expansion it folds every known alias onto its canonical
// ecosystem (gradle onto maven), built from the same table the derivation
// uses, so a pair declared under two aliases is one DISTINCT, ranked and
// rotated row instead of two. The literals come from a static, test-pinned
// table and are never caller input.
func ownedPackageEcosystemExpression(expandAliases bool) string {
	raw := "LOWER(fact.payload->'entity_metadata'->>'package_manager')"
	if !expandAliases {
		return raw
	}
	var expression strings.Builder
	expression.WriteString("CASE ")
	expression.WriteString(raw)
	for _, entry := range workflow.OwnedPackageManagerAliasTable() {
		fmt.Fprintf(&expression, " WHEN '%s' THEN '%s'", entry.Alias, entry.Canonical)
	}
	expression.WriteString(" ELSE ")
	expression.WriteString(raw)
	expression.WriteString(" END")
	return expression.String()
}

// ownedPackageManagerFilterValues returns the raw package_manager values the
// reader selects for the requested ecosystems. With alias expansion each
// ecosystem brings its aliases; without it the values pass through unchanged.
func ownedPackageManagerFilterValues(ecosystems []string, expandAliases bool) []string {
	if !expandAliases {
		return ecosystems
	}
	values := make([]string, 0, len(ecosystems)+2)
	for _, ecosystem := range ecosystems {
		values = append(values, workflow.OwnedPackageManagerAliases(ecosystem)...)
	}
	slices.Sort(values)
	return slices.Compact(values)
}

// ownedPackageSlotSelectMarker begins the final SELECT of the owned-package
// statement. Everything before it is the CTE chain that ranks, hashes and
// sizes the slots; the query-shape differential derives its read-only
// all-slots tail from the text before this marker, so it can never drift from
// production.
const ownedPackageSlotSelectMarker = "SELECT\n    window_targets.ecosystem,\n    window_targets.package_name,\n    window_targets.version,"

// ownedPackageActiveGenerationPredicate restricts the fact read to the active
// generation set (#9426). It is implied by the active-scope and active-status
// joins, so it removes no row those joins keep; it exists so the planner reads
// only active-generation fact rows instead of every generation's rows.
const ownedPackageActiveGenerationPredicate = "  AND fact.generation_id = ANY (ARRAY(SELECT active_generation_id FROM ingestion_scopes WHERE active_generation_id IS NOT NULL))\n"

// ownedPackageSlotCountExpression is P in SQL, the fill rule of
// [workflow.OwnedPackageTargetSlotCount] over the column total_targets and the
// target limit $4: 1 << bit_length(max(ceil(2T/L), 1) - 1), or 1 when $4 is
// zero or less. The statement renders it twice (per target, and on the
// one-row window stats) from this one definition, so the two cannot differ.
const ownedPackageSlotCountExpression = `CASE WHEN $4::bigint <= 0 THEN 1::bigint
      ELSE (1::bigint << length(ltrim((GREATEST((2 * total_targets + $4::bigint - 1) / NULLIF($4::bigint, 0), 1) - 1)::bit(64)::text, '0')))
    END AS slot_count`

// listOwnedPackageDependencyTargetsQuery builds the hash-slot window read
// (#9422). Each distinct target hashes to a fixed slot,
// sha256(key) low 24 bits mod P, where the key is the DISTINCT ON identity and
// P = next_pow2(ceil(2T/L)) is computed from T = COUNT(*) OVER () in the same
// statement. The window takes slot bucket mod P in the global target order, so
// a change in T never moves a target to a different window, and every target
// is planned within P windows. A one-row window_stats CTE computes T, P and the
// slot from COUNT(*) of the distinct targets and is LEFT JOINed to the slot's
// rows, so the statement returns the stats even when the slot, or the whole
// target set, is empty: an empty window is one row with NULL target columns.
// The CTE chain up to window_targets is the text the shim lane measured on the
// QA read replica; the stats CTE and the join were measured again on the same
// replica (docs/internal/evidence/9422-owned-package-hash-slot.md). The
// MATERIALIZED fence is kept for intent; it is not load-bearing on the
// measured plan because the slot predicate depends on the window aggregate.
// Binds: $1 raw package_manager values, $2 read limit, $3 rotation bucket,
// $4 target limit (zero or less means one slot).
func listOwnedPackageDependencyTargetsQuery(versionSpecific bool, expandAliases bool) string {
	distinctColumns := "ecosystem, package_name"
	distinctOrder := "ecosystem ASC, package_name ASC, CASE WHEN source_location <> '' THEN 0 ELSE 1 END ASC, lockfile DESC, version ASC, fact_id ASC"
	// COALESCE keeps a NULL version or name in a slot: sha256(NULL) is NULL,
	// which would put the target in no slot and starve it silently.
	slotKey := "COALESCE(ecosystem, '') || '|' || COALESCE(package_name, '')"
	if versionSpecific {
		distinctColumns = "ecosystem, package_name, version"
		distinctOrder = "ecosystem ASC, package_name ASC, version ASC, CASE WHEN source_location <> '' THEN 0 ELSE 1 END ASC, lockfile DESC, fact_id ASC"
		slotKey = "COALESCE(ecosystem, '') || '|' || COALESCE(package_name, '') || '|' || COALESCE(version, '')"
	}
	return fmt.Sprintf(ownedPackageSlotQueryTemplate,
		ownedPackageEcosystemExpression(expandAliases), distinctColumns, distinctOrder, slotKey,
		ownedPackageSlotCountExpression)
}

// ownedPackageSlotQueryTemplate is the measured statement with its five
// builder verbs: %[1]s ecosystem expression, %[2]s DISTINCT ON columns,
// %[3]s DISTINCT ON order, %[4]s slot key expression, %[5]s slot count.
const ownedPackageSlotQueryTemplate = `
WITH active_dependencies AS (
  SELECT
    %[1]s AS ecosystem,
    fact.payload->>'entity_name' AS package_name,
    fact.payload->'entity_metadata'->>'value' AS version,
    COALESCE((fact.payload->'entity_metadata'->>'lockfile') = 'true', FALSE) AS lockfile,
    COALESCE(fact.payload->>'repo_id', '') AS repository_id,
    fact.fact_id,
    COALESCE(fact.payload->'entity_metadata'->>'source_location', '') AS source_location
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'content_entity'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'entity_type' = 'Variable'
  AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
  AND fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])
  AND generation.status = 'active'
),
distinct_targets AS MATERIALIZED (
  SELECT DISTINCT ON (%[2]s)
    ecosystem,
    package_name,
    version,
    lockfile,
    repository_id,
    fact_id,
    source_location
  FROM active_dependencies
  ORDER BY %[3]s
),
numbered_targets AS (
  SELECT
    ecosystem,
    package_name,
    version,
    lockfile,
    repository_id,
    fact_id,
    source_location,
    ROW_NUMBER() OVER (
      ORDER BY ecosystem ASC, package_name ASC, version ASC, lockfile DESC, fact_id ASC
    ) - 1 AS target_rank,
    COUNT(*) OVER () AS total_targets,
    get_byte(key_hash, 0) + 256 * get_byte(key_hash, 1) + 65536 * get_byte(key_hash, 2) AS key_hash_low24
  FROM (
    SELECT
      distinct_targets.*,
      sha256(convert_to(%[4]s, 'UTF8')) AS key_hash
    FROM distinct_targets
  ) AS hashed_targets
),
sized_targets AS (
  SELECT
    numbered_targets.*,
    %[5]s
  FROM numbered_targets
),
slotted_targets AS (
  SELECT
    sized_targets.*,
    MOD(key_hash_low24::bigint, slot_count) AS slot_of_key,
    MOD($3::bigint, slot_count) AS window_slot
  FROM sized_targets
),
window_targets AS (
  SELECT
    slotted_targets.*,
    COUNT(*) OVER (PARTITION BY slot_of_key) AS slot_size
  FROM slotted_targets
),
window_stats AS (
  SELECT
    total_targets,
    slot_count,
    MOD($3::bigint, slot_count) AS window_slot
  FROM (
    SELECT
      total_targets,
      %[5]s
    FROM (SELECT COUNT(*) AS total_targets FROM distinct_targets) AS counted_targets
  ) AS sized_stats
)
SELECT
    window_targets.ecosystem,
    window_targets.package_name,
    window_targets.version,
    window_targets.lockfile,
    window_targets.repository_id,
    window_targets.fact_id,
    window_targets.source_location,
    window_stats.total_targets,
    window_stats.slot_count,
    window_stats.window_slot,
    COALESCE(window_targets.slot_size, 0) AS slot_size
FROM window_stats
LEFT JOIN window_targets
  ON window_targets.slot_of_key = window_stats.window_slot
ORDER BY window_targets.target_rank ASC
LIMIT $2
`

// ListOwnedPackageDependencyTargets loads active Git dependency declarations
// that can bound package-registry and vulnerability-intelligence target
// planning. It returns one hash slot of the target set (slot
// filter.RotationBucket mod P) in the global target order, with the window
// stats beside the rows. A read whose slot holds no targets returns no targets
// and still reports T, P and the slot with slot size 0, because the statement
// always returns its one-row window stats.
func (s FactStore) ListOwnedPackageDependencyTargets(
	ctx context.Context,
	filter workflow.OwnedPackageDependencyTargetFilter,
) (workflow.OwnedPackageDependencyTargetPage, error) {
	if s.database == nil {
		return workflow.OwnedPackageDependencyTargetPage{}, fmt.Errorf("fact store database is required")
	}
	ecosystems := cleanStringFilterValues(filter.Ecosystems)
	if len(ecosystems) == 0 {
		return workflow.OwnedPackageDependencyTargetPage{}, nil
	}
	limit := ownedPackageDependencyTargetLimit(filter.Limit)
	targetLimit := max(filter.TargetLimit, 0)
	bucket := max(filter.RotationBucket, 0)

	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(ctx, telemetry.SpanOwnedPackageTargetsRead)
	defer span.End()
	page, err := s.readOwnedPackageWindow(ctx, filter, ecosystems, limit, bucket, targetLimit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "owned package target read failed")
		return workflow.OwnedPackageDependencyTargetPage{}, err
	}
	recordOwnedPackageWindow(span, page)
	return page, nil
}

func (s FactStore) readOwnedPackageWindow(
	ctx context.Context,
	filter workflow.OwnedPackageDependencyTargetFilter,
	ecosystems []string,
	limit int,
	bucket int64,
	targetLimit int,
) (workflow.OwnedPackageDependencyTargetPage, error) {
	rows, err := s.database.QueryContext(
		ctx,
		listOwnedPackageDependencyTargetsQuery(filter.VersionSpecific, filter.ExpandPackageManagerAliases),
		ownedPackageManagerFilterValues(ecosystems, filter.ExpandPackageManagerAliases),
		limit,
		bucket,
		targetLimit,
	)
	if err != nil {
		return workflow.OwnedPackageDependencyTargetPage{}, fmt.Errorf("list owned package dependency targets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	page := workflow.OwnedPackageDependencyTargetPage{
		Targets: make([]workflow.OwnedPackageDependencyTarget, 0, limit),
	}
	statsRows := 0
	for rows.Next() {
		var (
			ecosystem, name, version, repositoryID, factID, sourceLocation sql.NullString
			lockfile                                                       sql.NullBool
		)
		if err := rows.Scan(
			&ecosystem,
			&name,
			&version,
			&lockfile,
			&repositoryID,
			&factID,
			&sourceLocation,
			&page.Window.TotalTargets,
			&page.Window.SlotCount,
			&page.Window.Slot,
			&page.Window.SlotSize,
		); err != nil {
			return workflow.OwnedPackageDependencyTargetPage{}, fmt.Errorf("list owned package dependency targets: %w", err)
		}
		statsRows++
		// The LEFT JOIN returns one row with NULL target columns when the
		// slot is empty: it carries the window stats and no target.
		if !factID.Valid {
			continue
		}
		page.Window.RowsRead++
		target := workflow.OwnedPackageDependencyTarget{
			Ecosystem:      strings.ToLower(strings.TrimSpace(ecosystem.String)),
			PackageName:    strings.TrimSpace(name.String),
			Version:        strings.TrimSpace(version.String),
			Lockfile:       lockfile.Bool,
			RepositoryID:   repositoryID.String,
			FactID:         factID.String,
			SourceLocation: strings.TrimSpace(sourceLocation.String),
		}
		if target.Ecosystem == "" || target.PackageName == "" {
			continue
		}
		page.Targets = append(page.Targets, target)
	}
	if err := rows.Err(); err != nil {
		return workflow.OwnedPackageDependencyTargetPage{}, fmt.Errorf("list owned package dependency targets: %w", err)
	}
	if statsRows == 0 {
		return workflow.OwnedPackageDependencyTargetPage{}, fmt.Errorf(
			"list owned package dependency targets: statement returned no window stats row")
	}
	if err := checkOwnedPackageWindow(page.Window, bucket, targetLimit); err != nil {
		return workflow.OwnedPackageDependencyTargetPage{}, err
	}
	return page, nil
}

// recordOwnedPackageWindow puts the window stats on the read span and adds
// one window event with the per-ecosystem selected counts. Ecosystems are a
// closed, configured set, so the attribute arrays stay small.
func recordOwnedPackageWindow(span trace.Span, page workflow.OwnedPackageDependencyTargetPage) {
	ecosystems, selected := page.SelectedByEcosystem()
	attributes := []attribute.KeyValue{
		attribute.Int64("owned_package_targets.total_targets", page.Window.TotalTargets),
		attribute.Int64("owned_package_targets.slot_count", page.Window.SlotCount),
		attribute.Int64("owned_package_targets.slot", page.Window.Slot),
		attribute.Int64("owned_package_targets.slot_size", page.Window.SlotSize),
		attribute.Int64("owned_package_targets.rows_read", page.Window.RowsRead),
		attribute.Int("owned_package_targets.selected", len(page.Targets)),
		attribute.Bool("owned_package_targets.truncated", page.Window.Truncated()),
	}
	span.SetAttributes(attributes...)
	span.AddEvent(telemetry.EventOwnedPackageTargetsWindow, trace.WithAttributes(append(attributes,
		attribute.StringSlice("owned_package_targets.ecosystems", ecosystems),
		attribute.IntSlice("owned_package_targets.selected_by_ecosystem", selected),
	)...))
}

// checkOwnedPackageWindow fails the read when the statement's slot count or
// slot disagrees with the Go fill rule, so the SQL and Go definitions of P
// cannot drift apart silently. Every read carries the stats, including an
// empty slot and T = 0 (P = 1).
func checkOwnedPackageWindow(window workflow.OwnedPackageDependencyTargetWindow, bucket int64, targetLimit int) error {
	if want := workflow.OwnedPackageTargetSlotCount(window.TotalTargets, targetLimit); window.SlotCount != want {
		return fmt.Errorf("list owned package dependency targets: statement slot count %d for T=%d L=%d, want %d",
			window.SlotCount, window.TotalTargets, targetLimit, want)
	}
	if want := bucket % window.SlotCount; window.Slot != want {
		return fmt.Errorf("list owned package dependency targets: statement slot %d for bucket %d mod %d, want %d",
			window.Slot, bucket, window.SlotCount, want)
	}
	return nil
}

func ownedPackageDependencyTargetLimit(raw int) int {
	if raw <= 0 {
		return defaultOwnedPackageDependencyTargetLimit
	}
	if raw > maxOwnedPackageDependencyTargetLimit {
		return maxOwnedPackageDependencyTargetLimit
	}
	return raw
}
