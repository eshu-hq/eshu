// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// generationFetchPageSize bounds one generation-fact page (#11432). It
// matches factBatchSize and listFactsByKindPageSize so the projector avoids
// thousands of tiny round trips while each statement stays a short keyset
// seek: the theory shim measured at most 1.4 ms warm per 500-row page on a
// 100k-fact fixture (10x headroom: 14 ms), and a 2000-row page at 3.7 ms
// (10x: 37 ms). Both fit the per-page timeout with wide margin; 500 keeps
// the per-statement blast radius smallest for the same total memory, since
// the projector accumulates the whole generation either way.
const generationFetchPageSize = 500

// generationFetchPageTimeout bounds one generation-fact page statement. The
// theory shim measured at most 1.4 ms warm per page at generationFetchPageSize
// on a 100k-fact fixture under a forced generic plan, so 30 s carries more
// than 2000x headroom while staying under the 60 s cap the ruling sets. It is
// a per-statement bound, not a per-generation deadline: every page statement
// runs under it, and a slow page fails fast instead of pinning a connection
// until the 5-minute reducer-admission gate fires.
const generationFetchPageTimeout = 30 * time.Second

// setGenerationFetchTimeoutQuery scopes the statement timeout to the fetch
// transaction, so a pooled connection never carries it away. It mirrors
// setSupplyChainImpactFanOutReadTimeoutQuery.
const setGenerationFetchTimeoutQuery = `SELECT set_config('statement_timeout', $1, true)`

// listFactsFirstPageQuery reads the first generation-fact page. The first
// page and the cursor pages are separate statements on purpose (#6154): a
// folded `OR cursor IS NULL` shape demotes the row comparison to a filter
// under a generic plan and rescans the generation once per page.
const listFactsFirstPageQuery = `
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
ORDER BY observed_at ASC, fact_id ASC
LIMIT $3
`

// listFactsCursorPageQuery reads one generation-fact page after cursor. The
// row comparison is a plain predicate so it stays an index qual on
// fact_records_scope_generation_keyset_idx (migration 099) under a generic
// plan; see listFactsByKindCursorQuery for the same shape.
const listFactsCursorPageQuery = `
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
  AND (observed_at, fact_id) > ($3, $4)
ORDER BY observed_at ASC, fact_id ASC
LIMIT $5
`

// listFactsGeneration runs the keyset-chunked generation fetch (#11432): one
// read-only repeatable-read transaction per call, one statement timeout
// scoping every page statement, and LIMIT pages ordered by
// (observed_at, fact_id) where each page resumes after the previous page's
// last row. A page shorter than generationFetchPageSize ends the fetch; a
// page that dies partway fails the fetch, never a short success with the rows
// read so far.
func (s FactStore) listFactsGeneration(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	var loaded []facts.Envelope
	err := withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		timeoutRows, err := queryer.QueryContext(ctx, setGenerationFetchTimeoutQuery,
			fmt.Sprintf("%dms", generationFetchPageTimeout.Milliseconds()))
		if err != nil {
			return classifyGenerationFetchError("set generation fetch statement timeout", err)
		}
		_ = timeoutRows.Close()
		pages, err := listFactsPages(ctx, queryer, scopeID, generationID)
		if err != nil {
			return err
		}
		loaded = pages
		return nil
	})
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

// listFactsPages reads every generation-fact page on the caller's snapshot:
// the first page, then cursor pages until a short page ends the fetch.
func listFactsPages(
	ctx context.Context,
	queryer db.Queryer,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	var loaded []facts.Envelope
	query := listFactsFirstPageQuery
	args := []any{scopeID, generationID, generationFetchPageSize}
	for {
		page, err := scanFactsPage(ctx, queryer, query, args...)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < generationFetchPageSize {
			return loaded, nil
		}
		last := page[len(page)-1]
		query = listFactsCursorPageQuery
		args = []any{scopeID, generationID, last.ObservedAt, last.FactID, generationFetchPageSize}
	}
}

// scanFactsPage runs one generation-fact page statement and scans its rows.
// A cancelled page (SQLSTATE 57014) or a context deadline classifies to the
// typed fetch timeout; any other failure keeps the list-facts context.
func scanFactsPage(
	ctx context.Context,
	queryer db.Queryer,
	query string,
	args ...any,
) ([]facts.Envelope, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, classifyGenerationFetchError("list facts", err)
	}
	defer func() { _ = rows.Close() }()
	var page []facts.Envelope
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list facts: %w", scanErr)
		}
		page = append(page, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyGenerationFetchError("list facts", err)
	}
	return page, nil
}

// classifyGenerationFetchError wraps a statement-timeout failure (SQLSTATE
// 57014) or a context deadline in projector.ErrGenerationFetchTimeout,
// mirroring classifyFanOutReadError. Any other failure keeps the operation
// context. A dying page fails the fetch: returning a typed error here (and
// never the rows read so far) is what forbids a partial page as success.
func classifyGenerationFetchError(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &pgErr) && pgErr.Code == "57014") {
		return fmt.Errorf("%w: %v", projector.ErrGenerationFetchTimeout, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}
