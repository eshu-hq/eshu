// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package statestore

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// listTerraformStateLastSerials returns the most recent observed serial per
// state_snapshot scope keyed by safe_locator_hash. The query bounds itself to
// active or pending generations and is bounded by the number of distinct
// Terraform-state scopes; no per-locator limit is needed because the result
// already reduces to one row per locator.
func listTerraformStateLastSerials(
	ctx context.Context,
	queryer db.Queryer,
) ([]statuspkg.TerraformStateLocatorSerial, error) {
	rows, err := queryer.QueryContext(ctx, terraformStateLastSerialQuery)
	if err != nil {
		return nil, fmt.Errorf("list terraform state last serials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	serials := []statuspkg.TerraformStateLocatorSerial{}
	for rows.Next() {
		var locatorHash string
		var backendKind string
		var lineage string
		var serialText string
		var generationID string
		var observedAt sql.NullTime
		if scanErr := rows.Scan(
			&locatorHash,
			&backendKind,
			&lineage,
			&serialText,
			&generationID,
			&observedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("list terraform state last serials: %w", scanErr)
		}
		serial, parseErr := strconv.ParseInt(strings.TrimSpace(serialText), 10, 64)
		if parseErr != nil {
			// Skip rows with malformed generation IDs rather than failing the
			// whole admin status query; this is observability data.
			continue
		}
		row := statuspkg.TerraformStateLocatorSerial{
			SafeLocatorHash: strings.TrimSpace(locatorHash),
			BackendKind:     strings.TrimSpace(backendKind),
			Lineage:         strings.TrimSpace(lineage),
			Serial:          serial,
			GenerationID:    strings.TrimSpace(generationID),
		}
		if observedAt.Valid {
			row.ObservedAt = observedAt.Time.UTC()
		}
		serials = append(serials, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list terraform state last serials: %w", err)
	}
	return serials, nil
}

// listTerraformStateRecentWarnings returns up to limit warning_fact rows per
// safe_locator_hash. Limit must be positive; callers that want the contract
// default should pass statuspkg.MaxTerraformStateRecentWarnings.
func listTerraformStateRecentWarnings(
	ctx context.Context,
	queryer db.Queryer,
	limit int,
) ([]statuspkg.TerraformStateLocatorWarning, error) {
	if limit <= 0 {
		limit = statuspkg.MaxTerraformStateRecentWarnings
	}
	rows, err := queryer.QueryContext(ctx, terraformStateRecentWarningsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("list terraform state recent warnings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	warnings := []statuspkg.TerraformStateLocatorWarning{}
	for rows.Next() {
		var locatorHash string
		var backendKind string
		var warningKind string
		var reason string
		var severity string
		var actionability string
		var source string
		var sourceHandle string
		var generationID string
		var observedAt sql.NullTime
		if scanErr := rows.Scan(
			&locatorHash,
			&backendKind,
			&warningKind,
			&reason,
			&severity,
			&actionability,
			&source,
			&sourceHandle,
			&generationID,
			&observedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("list terraform state recent warnings: %w", scanErr)
		}
		row := statuspkg.TerraformStateLocatorWarning{
			SafeLocatorHash: strings.TrimSpace(locatorHash),
			BackendKind:     strings.TrimSpace(backendKind),
			WarningKind:     strings.TrimSpace(warningKind),
			Reason:          strings.TrimSpace(reason),
			Severity:        strings.TrimSpace(severity),
			Actionability:   strings.TrimSpace(actionability),
			Source:          strings.TrimSpace(source),
			SourceHandle:    strings.TrimSpace(sourceHandle),
			GenerationID:    strings.TrimSpace(generationID),
		}
		if observedAt.Valid {
			row.ObservedAt = observedAt.Time.UTC()
		}
		warnings = append(warnings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list terraform state recent warnings: %w", err)
	}
	return warnings, nil
}

// TerraformStateAdminEvidence is the bounded admin-status evidence shape
// returned by ReadTerraformStateAdminEvidence. Callers can either use the
// helper or inline the two list calls when they need to control timing.
type TerraformStateAdminEvidence struct {
	LastSerials    []statuspkg.TerraformStateLocatorSerial
	RecentWarnings []statuspkg.TerraformStateLocatorWarning
}

// ReadTerraformStateAdminEvidence calls both query helpers in a single call so
// status readers can populate RawSnapshot.TerraformStateLastSerials and
// RawSnapshot.TerraformStateRecentWarnings in one place. Returns no error when
// either list is empty so admin status remains useful even on a fresh
// database. It stays exported because the status family (still in the
// postgres root until its own #6693 leaf) reads through it.
func ReadTerraformStateAdminEvidence(
	ctx context.Context,
	queryer db.Queryer,
	limit int,
	asOf time.Time,
) (TerraformStateAdminEvidence, error) {
	_ = asOf // reserved for future bounded-window queries.
	serials, err := listTerraformStateLastSerials(ctx, queryer)
	if err != nil {
		return TerraformStateAdminEvidence{}, err
	}
	warnings, err := listTerraformStateRecentWarnings(ctx, queryer, limit)
	if err != nil {
		return TerraformStateAdminEvidence{}, err
	}
	return TerraformStateAdminEvidence{
		LastSerials:    serials,
		RecentWarnings: warnings,
	}, nil
}

// terraformStateLastSerialQuery returns the latest observed serial per
// state_snapshot scope. Lineage and serial are extracted from the
// generation_id pattern terraform_state:{scope_id}:{lineage}:serial:{serial}
// produced by scope.NewTerraformStateSnapshotGeneration. The query bounds
// itself to active or pending generations and prefers the most recent
// ingested_at so superseded generations are not picked when the active one
// is rolling forward. Result is bounded by the total number of state
// snapshot scopes (one row per scope).
const terraformStateLastSerialQuery = `
WITH ranked_generations AS (
    SELECT
        scope.scope_id AS scope_id,
        COALESCE(scope.payload->>'locator_hash', '') AS locator_hash,
        COALESCE(scope.payload->>'backend_kind', '') AS backend_kind,
        generation.generation_id AS generation_id,
        generation.ingested_at AS ingested_at,
        generation.observed_at AS observed_at,
        substring(generation.generation_id from 'terraform_state:[^:]+:[^:]+:[^:]+:([^:]+):serial:[0-9]+$') AS lineage_uuid,
        substring(generation.generation_id from 'serial:([0-9]+)$') AS serial_text,
        ROW_NUMBER() OVER (
            PARTITION BY scope.scope_id
            ORDER BY generation.ingested_at DESC, generation.generation_id DESC
        ) AS rank
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
        ON generation.scope_id = scope.scope_id
    WHERE scope.scope_kind = 'state_snapshot'
      AND scope.collector_kind = 'terraform_state'
      AND generation.status IN ('active', 'pending', 'superseded')
      AND generation.generation_id LIKE 'terraform_state:%:serial:%'
)
SELECT
    locator_hash,
    backend_kind,
    COALESCE(lineage_uuid, ''),
    COALESCE(serial_text, '0'),
    generation_id,
    observed_at
FROM ranked_generations
WHERE rank = 1
  AND locator_hash <> ''
ORDER BY locator_hash ASC
`

// terraformStateRecentWarningsQuery returns the N most recent warning_fact
// rows per safe locator or Git source handle. The N bound is enforced via
// window-function ranking so the result size is hard-capped regardless of how
// many warning facts a single state or backend source has accumulated.
const terraformStateRecentWarningsQuery = `
WITH raw_warning_rows AS (
    SELECT
        CASE
            WHEN scope.collector_kind = 'git'
             AND scope.scope_kind = 'repository'
             AND fact.payload->>'warning_kind' = 'unresolved_backend_expression'
             AND COALESCE(fact.payload->>'repo_id', '') <> ''
             AND COALESCE(fact.payload->>'source_path', '') <> ''
            THEN CONCAT(fact.payload->>'repo_id', ':', fact.payload->>'source_path')
            ELSE COALESCE(scope.payload->>'locator_hash', '')
        END AS locator_hash,
        CASE
            WHEN scope.collector_kind = 'git'
             AND scope.scope_kind = 'repository'
             AND fact.payload->>'warning_kind' = 'unresolved_backend_expression'
            THEN 'git'
            ELSE COALESCE(scope.payload->>'backend_kind', '')
        END AS backend_kind,
        COALESCE(fact.payload->>'warning_kind', '') AS warning_kind,
        COALESCE(fact.payload->>'reason', '') AS reason,
        COALESCE(fact.payload->>'severity', '') AS severity,
        COALESCE(fact.payload->>'actionability', '') AS actionability,
        COALESCE(fact.payload->>'source', '') AS source,
        CASE
            WHEN scope.collector_kind = 'git'
             AND scope.scope_kind = 'repository'
             AND fact.payload->>'warning_kind' = 'unresolved_backend_expression'
            THEN COALESCE(fact.payload->>'source_path', '')
            ELSE COALESCE(fact.payload->>'source_handle', '')
        END AS source_handle,
        fact.generation_id AS generation_id,
        fact.observed_at AS observed_at,
        fact.fact_id AS fact_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
        ON scope.scope_id = fact.scope_id
    WHERE fact.fact_kind = 'terraform_state_warning'
      AND scope.scope_kind IN ('state_snapshot', 'repository')
      AND scope.collector_kind IN ('terraform_state', 'git')
      AND (
        (scope.scope_kind = 'state_snapshot' AND scope.collector_kind = 'terraform_state')
        OR (
            scope.scope_kind = 'repository'
            AND scope.collector_kind = 'git'
            AND fact.payload->>'warning_kind' = 'unresolved_backend_expression'
        )
      )
), warning_rows AS (
    SELECT
        locator_hash,
        backend_kind,
        warning_kind,
        reason,
        severity,
        actionability,
        source,
        source_handle,
        generation_id,
        observed_at,
        ROW_NUMBER() OVER (
            PARTITION BY locator_hash
            ORDER BY observed_at DESC, fact_id DESC
        ) AS rank
    FROM raw_warning_rows
)
SELECT locator_hash, backend_kind, warning_kind, reason, severity, actionability, source, source_handle, generation_id, observed_at
FROM warning_rows
WHERE rank <= $1
  AND locator_hash <> ''
  AND warning_kind <> ''
ORDER BY locator_hash ASC, warning_kind ASC, observed_at DESC
`
