// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listActiveRepositoryFactsQuery reads the cross-scope repository catalog. Its
// payload column subtracts, in SQL, exactly the keys
// facts.RepositoryCatalogOmittedPayloadKeys names (issue #9567), so the
// reducer never transfers, scans, or decodes imports_map (most of a ~94 KB
// repository payload), git_refs, or the delta path arrays. It subtracts only
// from an object payload: jsonb "-" raises on a scalar or JSON null, and one
// such row would fail the whole load for every consumer domain, so any other
// payload passes through unchanged. The type check and the subtraction each
// detoast a toasted argument, so the lateral "#> '{}'" (an identity path
// read) detoasts the payload once for both; reading fact.payload in each made
// page 1 69% slower than the unprojected read on PG18. A projection test
// derives the expected expression from the key list.
const listActiveRepositoryFactsQuery = `
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
    CASE WHEN jsonb_typeof(detoasted.payload) = 'object'
         THEN detoasted.payload - 'imports_map' - 'git_refs' - 'delta_relative_paths' - 'delta_deleted_relative_paths'
         ELSE detoasted.payload END
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
CROSS JOIN LATERAL (SELECT fact.payload #> '{}' AS payload OFFSET 0) AS detoasted
WHERE fact.fact_kind = 'repository'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    $1::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $3
`

// ListActiveRepositoryFacts loads active, non-tombstoned repository facts across
// Git-owned scopes. Reducer correlation domains use this as the cross-scope
// repository catalog instead of scanning source-local package-registry
// generations.
//
// The catalog is an identity catalog, not the full fact: each payload omits
// the keys facts.RepositoryCatalogOmittedPayloadKeys names (imports_map,
// git_refs, delta_relative_paths, delta_deleted_relative_paths). Every other
// key, including any new small identity key, still flows through. A caller
// that needs an omitted key must use a separate method named for the full
// payload; TestRepositoryCatalogConsumersReadNoProjectedKey fails if a catalog
// consumer reads one.
//
// The query filters fact.is_tombstone = FALSE so a repository fact tombstoned
// within a still-active generation is never surfaced as live. The active
// generation pointer keeps tombstoned rows joinable, so the predicate is the
// only guard against returning superseded repository identities to correlation
// consumers. This matches every sibling active source-local reader.
//
// No-Regression Evidence: predicate-only narrowing of an existing WHERE clause
// on the active-generation read. The partial fact_records_active_repository_idx
// (observed_at, fact_id, generation_id WHERE fact_kind='repository' AND
// source_system='git') does not cover is_tombstone, so the planner applies the
// new predicate as a residual filter on rows the index scan already visits; the
// index choice, the active-generation join, and the scan shape are unchanged.
// go test ./internal/storage/postgres ./internal/reducer -count=1.
//
// Performance Evidence (#9567): QA OOM-killed the reducer three times while
// container_image_identity and supply_chain_impact each reloaded this catalog
// per intent (799 facts, ~94 KB mean payload, ~71 MiB of JSON). A shim driving
// the real handlers with 8 workers measured peak HeapAlloc 1,426 MiB (median of
// 3, mixed wave) before and 142 MiB after the projection, and 227.5 vs 75.8 MiB
// allocated per intent, with byte-identical decisions and findings. On a
// QA-shaped native PG18 seed (799 facts, 95.5 KB mean) EXPLAIN (ANALYZE,
// BUFFERS, SERIALIZE) of this statement against the unprojected read, 12
// interleaved runs, gave p50 execution 122.9 -> 108.5 ms for page 1 and
// 75.3 -> 45.7 ms for page 2, with 46.8 MB -> 0.3 MB serialized per page; the
// plan keeps its scans, joins, and index plus a per-row Result node. See
// docs/public/reference/telemetry/streaming-memory.md (Repository Catalog Loads).
// Observability Evidence: eshu_dp_repository_catalog_load_rows and
// eshu_dp_repository_catalog_load_payload_bytes record each load, labeled by
// the calling reducer domain.
func (s FactStore) ListActiveRepositoryFacts(ctx context.Context) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	var loaded []facts.Envelope
	var payloadBytes int64
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, pageBytes, err := s.listActiveRepositoryFactsPage(ctx, cursorObservedAt, cursorFactID)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		payloadBytes += pageBytes
		if len(page) < listFactsByKindPageSize {
			s.recordRepositoryCatalogLoad(ctx, len(loaded), payloadBytes)
			return loaded, nil
		}

		last := page[len(page)-1]
		observedAt := last.ObservedAt.UTC()
		cursorObservedAt = &observedAt
		cursorFactID = last.FactID
	}
}

func (s FactStore) listActiveRepositoryFactsPage(
	ctx context.Context,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, int64, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	rows, err := s.database.QueryContext(
		ctx,
		listActiveRepositoryFactsQuery,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list active repository facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	var payloadBytes int64
	for rows.Next() {
		envelope, size, scanErr := backfill.ScanFactEnvelopeWithPayloadSize(rows)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("list active repository facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
		payloadBytes += int64(size)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list active repository facts: %w", err)
	}

	return loaded, payloadBytes, nil
}

// repositoryCatalogDomains is the closed label set for the
// eshu_dp_repository_catalog_load_* histograms: the reducer domains that read
// the catalog. Any other context value is recorded as "unknown".
var repositoryCatalogDomains = map[string]struct{}{
	string(reducercontract.DomainContainerImageIdentity):      {},
	string(reducercontract.DomainSupplyChainImpact):           {},
	string(reducercontract.DomainPackageSourceCorrelation):    {},
	string(reducercontract.DomainServiceCatalogCorrelation):   {},
	string(reducercontract.DomainSecurityAlertReconciliation): {},
}

// recordRepositoryCatalogLoad records one successful catalog load. It is a
// no-op when the store has no instruments.
func (s FactStore) recordRepositoryCatalogLoad(ctx context.Context, rows int, payloadBytes int64) {
	if s.instruments == nil {
		return
	}
	domain := telemetry.RepositoryCatalogDomain(ctx)
	if _, ok := repositoryCatalogDomains[domain]; !ok {
		domain = "unknown"
	}
	attrs := metric.WithAttributes(telemetry.AttrDomain(domain))
	if s.instruments.RepositoryCatalogLoadRows != nil {
		s.instruments.RepositoryCatalogLoadRows.Record(ctx, int64(rows), attrs)
	}
	if s.instruments.RepositoryCatalogLoadBytes != nil {
		s.instruments.RepositoryCatalogLoadBytes.Record(ctx, payloadBytes, attrs)
	}
}
