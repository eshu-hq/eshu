// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/filters"
)

// listFactsByKindPageSize matches factBatchSize so reducer handlers avoid
// thousands of tiny Postgres round trips while still bounding each cursor page.
const listFactsByKindPageSize = factBatchSize

// The first page and the cursor pages are separate statements on purpose.
//
// Every fact in a generation carries the same observed_at, so a page can only
// be bounded by fact_id, and that bound is only usable as an index qual when
// the row comparison is a plain predicate. Folding both cases into one
// statement with `$4::timestamptz IS NULL OR (observed_at, fact_id) > (...)`
// demotes the comparison to a filter under a generic plan -- which is where
// these statements end up, because the services prepare them through
// database/sql. Postgres then rescans the whole generation for every page and
// pagination becomes quadratic. Issue #6154 carries the plans and timings.
const listFactsByKindQuery = `
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
    collector_kind,
    fencing_token,
    source_confidence,
    source_system,
    source_fact_key,
    COALESCE(source_uri, ''),
    COALESCE(source_record_id, ''),
    observed_at,
    is_tombstone,
    payload
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = ANY($3::text[])
ORDER BY observed_at ASC, fact_id ASC
LIMIT $4
`

const listFactsByKindCursorQuery = `
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
    collector_kind,
    fencing_token,
    source_confidence,
    source_system,
    source_fact_key,
    COALESCE(source_uri, ''),
    COALESCE(source_record_id, ''),
    observed_at,
    is_tombstone,
    payload
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = ANY($3::text[])
  AND (observed_at, fact_id) > ($4::timestamptz, $5::text)
ORDER BY observed_at ASC, fact_id ASC
LIMIT $6
`

// The protected impact loader obtains advisory facts from published public
// receipts and derived scan facts from result receipts in the same snapshot.
// This predicate is evaluated before both direct-scope page limits.
const protectedStaticScopeFactPredicate = `scope_id NOT LIKE 'image://derived/%'
  AND fact_kind NOT IN ('vulnerability.cve', 'vulnerability.affected_package',
                        'vulnerability.affected_product', 'vulnerability.epss_score',
                        'vulnerability.known_exploited')`

var (
	protectedStaticFactsByKindQuery = strings.Replace(listFactsByKindQuery,
		"WHERE scope_id =", "WHERE "+protectedStaticScopeFactPredicate+"\n  AND scope_id =", 1)
	protectedStaticFactsByKindCursorQuery = strings.Replace(listFactsByKindCursorQuery,
		"WHERE scope_id =", "WHERE "+protectedStaticScopeFactPredicate+"\n  AND scope_id =", 1)
)

// Split for the same reason as listFactsByKindQuery above.
const listFactsByKindAndPayloadValueQuery = `
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
    collector_kind,
    fencing_token,
    source_confidence,
    source_system,
    source_fact_key,
    COALESCE(source_uri, ''),
    COALESCE(source_record_id, ''),
    observed_at,
    is_tombstone,
    payload
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = $3
  AND payload ->> $4 = ANY($5::text[])
ORDER BY observed_at ASC, fact_id ASC
LIMIT $6
`

const listFactsByKindAndPayloadValueCursorQuery = `
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
    collector_kind,
    fencing_token,
    source_confidence,
    source_system,
    source_fact_key,
    COALESCE(source_uri, ''),
    COALESCE(source_record_id, ''),
    observed_at,
    is_tombstone,
    payload
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = $3
  AND payload ->> $4 = ANY($5::text[])
  AND (observed_at, fact_id) > ($6::timestamptz, $7::text)
ORDER BY observed_at ASC, fact_id ASC
LIMIT $8
`

var (
	protectedStaticFactsByPayloadQuery = strings.Replace(listFactsByKindAndPayloadValueQuery,
		"WHERE scope_id =", "WHERE "+protectedStaticScopeFactPredicate+"\n  AND scope_id =", 1)
	protectedStaticFactsByPayloadCursorQuery = strings.Replace(listFactsByKindAndPayloadValueCursorQuery,
		"WHERE scope_id =", "WHERE "+protectedStaticScopeFactPredicate+"\n  AND scope_id =", 1)
)

// ListFactsByKind loads fact envelopes for one scope generation and a bounded
// set of fact kinds, preserving the same stable ordering as ListFacts.
func (s FactStore) ListFactsByKind(
	ctx context.Context,
	scopeID string,
	generationID string,
	factKinds []string,
) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	factKinds = filters.CleanFactKinds(factKinds)
	if len(factKinds) == 0 {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listFactsByKindPage(ctx, scopeID, generationID, factKinds, cursorObservedAt, cursorFactID)
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

// ListFactsByKindAndPayloadValue loads one fact kind filtered by a top-level
// JSON payload key whose text value is in the provided allowlist.
func (s FactStore) ListFactsByKindAndPayloadValue(
	ctx context.Context,
	scopeID string,
	generationID string,
	factKind string,
	payloadKey string,
	payloadValues []string,
) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	factKind = strings.TrimSpace(factKind)
	if s.impactStaticSnapshot && (factKind == "scanner_worker.analysis" || factKind == "scanner_worker.warning" ||
		factKind == "vulnerability.os_package" || factKind == "vulnerability.warning") {
		return nil, fmt.Errorf("protected impact coverage unavailable: target-scoped payload-value read has no scan receipt authority")
	}
	payloadKey = strings.TrimSpace(payloadKey)
	payloadValues = filters.CleanFactKinds(payloadValues)
	if factKind == "" || payloadKey == "" || len(payloadValues) == 0 {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listFactsByKindAndPayloadValuePage(
			ctx,
			scopeID,
			generationID,
			factKind,
			payloadKey,
			payloadValues,
			cursorObservedAt,
			cursorFactID,
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

// listFactsByKindPage reads one stable-order page so large reducer inputs do
// not rely on a single long-lived database cursor.
func (s FactStore) listFactsByKindPage(
	ctx context.Context,
	scopeID string,
	generationID string,
	factKinds []string,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	query := listFactsByKindQuery
	if s.impactStaticSnapshot {
		query = protectedStaticFactsByKindQuery
	}
	args := []any{scopeID, generationID, factKinds, listFactsByKindPageSize}
	if cursorObservedAt != nil {
		query = listFactsByKindCursorQuery
		if s.impactStaticSnapshot {
			query = protectedStaticFactsByKindCursorQuery
		}
		args = []any{
			scopeID,
			generationID,
			factKinds,
			cursorObservedAt.UTC(),
			cursorFactID,
			listFactsByKindPageSize,
		}
	}

	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list facts by kind: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list facts by kind: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list facts by kind: %w", err)
	}

	return loaded, nil
}

func (s FactStore) listFactsByKindAndPayloadValuePage(
	ctx context.Context,
	scopeID string,
	generationID string,
	factKind string,
	payloadKey string,
	payloadValues []string,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	query := listFactsByKindAndPayloadValueQuery
	if s.impactStaticSnapshot {
		query = protectedStaticFactsByPayloadQuery
	}
	args := []any{
		scopeID,
		generationID,
		factKind,
		payloadKey,
		payloadValues,
		listFactsByKindPageSize,
	}
	if cursorObservedAt != nil {
		query = listFactsByKindAndPayloadValueCursorQuery
		if s.impactStaticSnapshot {
			query = protectedStaticFactsByPayloadCursorQuery
		}
		args = []any{
			scopeID,
			generationID,
			factKind,
			payloadKey,
			payloadValues,
			cursorObservedAt.UTC(),
			cursorFactID,
			listFactsByKindPageSize,
		}
	}

	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list facts by kind and payload value: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list facts by kind and payload value: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list facts by kind and payload value: %w", err)
	}

	return loaded, nil
}
