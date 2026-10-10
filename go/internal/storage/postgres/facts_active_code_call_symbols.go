// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
)

// codeCallSymbolDefinitionFactsSelect is the shared head of the definition
// scans: active, non-tombstoned file facts of each scope's active generation.
//
// The LATERAL subquery reads the file's parsed_file_data once. Reading it
// through fact.payload in each of the ten places the match uses it detoasts the
// large out-of-line payload ten times per file, which was most of the scan's
// cost (about 0.27 ms per producer file fact on the shared QA replica, #7601).
// OFFSET 0 stops the planner from folding the subquery back into its callers.
const codeCallSymbolDefinitionFactsSelect = `
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
CROSS JOIN LATERAL (
  SELECT fact.payload->'parsed_file_data' AS pfd
  OFFSET 0
) AS parsed
WHERE fact.fact_kind = 'file'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
`

// codeCallSymbolDefinitionFactsMatch is the shared tail of the definition
// scans: the per-definition key match plus the (observed_at, fact_id) keyset
// page. $1 is the key list, $2/$3 the cursor, and $4 the page size.
const codeCallSymbolDefinitionFactsMatch = `  AND EXISTS (
    SELECT 1
    FROM (
      SELECT definition.item
      FROM jsonb_array_elements(
        CASE
          WHEN jsonb_typeof(parsed.pfd->'functions') = 'array'
          THEN parsed.pfd->'functions'
          ELSE '[]'::jsonb
        END
      ) AS definition(item)
      UNION ALL
      SELECT definition.item
      FROM jsonb_array_elements(
        CASE
          WHEN jsonb_typeof(parsed.pfd->'classes') = 'array'
          THEN parsed.pfd->'classes'
          ELSE '[]'::jsonb
        END
      ) AS definition(item)
      UNION ALL
      SELECT definition.item
      FROM jsonb_array_elements(
        CASE
          WHEN jsonb_typeof(parsed.pfd->'structs') = 'array'
          THEN parsed.pfd->'structs'
          ELSE '[]'::jsonb
        END
      ) AS definition(item)
      UNION ALL
      SELECT definition.item
      FROM jsonb_array_elements(
        CASE
          WHEN jsonb_typeof(parsed.pfd->'interfaces') = 'array'
          THEN parsed.pfd->'interfaces'
          ELSE '[]'::jsonb
        END
      ) AS definition(item)
      UNION ALL
      SELECT definition.item
      FROM jsonb_array_elements(
        CASE
          WHEN jsonb_typeof(parsed.pfd->'type_aliases') = 'array'
          THEN parsed.pfd->'type_aliases'
          ELSE '[]'::jsonb
        END
      ) AS definition(item)
    ) AS code_definition(item)
    WHERE code_definition.item->>'scip_symbol' = ANY($1::text[])
       OR code_definition.item->>'scip_symbol_key' = ANY($1::text[])
       OR code_definition.item->>'scip_moniker' = ANY($1::text[])
       OR code_definition.item->>'symbol' = ANY($1::text[])
       OR code_definition.item->>'package_export_symbol' = ANY($1::text[])
       OR code_definition.item->>'export_symbol' = ANY($1::text[])
       OR code_definition.item->>'stable_symbol_key' = ANY($1::text[])
       OR (
         COALESCE(NULLIF(code_definition.item->>'package_id', ''), '') <> ''
         AND COALESCE(NULLIF(COALESCE(code_definition.item->>'export_name', code_definition.item->>'exported_name'), ''), '') <> ''
         AND (
           'package:' || (code_definition.item->>'package_id') || '#' ||
           COALESCE(code_definition.item->>'export_name', code_definition.item->>'exported_name')
         ) = ANY($1::text[])
       )
  )
  AND (
    $2::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($2::timestamptz, $3::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $4
`

// listActiveCodeCallSymbolDefinitionFactsQuery scans every active file fact in
// the corpus. It serves only keys no stored manifest can anchor: SCIP symbols
// from other indexers and a scip-go gomod key without a symbol. A Go key with no
// declaring module issues no definition scan at all.
const listActiveCodeCallSymbolDefinitionFactsQuery = codeCallSymbolDefinitionFactsSelect +
	codeCallSymbolDefinitionFactsMatch

// listAnchoredActiveCodeCallSymbolDefinitionFactsQuery is the same scan
// restricted to the producer scopes in $5, so its cost follows the producer
// file facts instead of the whole corpus (#7601).
const listAnchoredActiveCodeCallSymbolDefinitionFactsQuery = codeCallSymbolDefinitionFactsSelect +
	"  AND fact.scope_id = ANY($5::text[])\n" +
	codeCallSymbolDefinitionFactsMatch

// LoadActiveCodeCallSymbolDefinitionFacts loads active file facts whose parsed
// definitions carry one of the requested stable symbol keys.
//
// Two kinds of key are anchored on the repositories that can define them, so
// the scan reads only those repositories' active file facts:
//
//   - package:<package_id>#<export_name> keys, on the repositories whose stored
//     package.json manifests name that package (#7601);
//   - scip-go gomod <import path> <symbol> keys, on the repositories whose
//     stored go.mod module path is that import path or a prefix of it (#7623).
//     The Go parser builds a definition's import path from its nearest go.mod
//     module path, so the defining repository always stores such a go.mod.
//
// A key with no producer issues no definition scan and stays unresolved. Every
// other key keeps the corpus-wide scan. The three groups run as separate scans,
// so a manifest that matches one group never widens another.
func (s FactStore) LoadActiveCodeCallSymbolDefinitionFacts(
	ctx context.Context,
	symbolKeys []string,
) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	symbolKeys = cleanFactKinds(symbolKeys)
	if len(symbolKeys) == 0 {
		return nil, nil
	}

	packageKeys, goKeys, otherKeys := producerstore.Split(symbolKeys)
	load := codeCallSymbolLoadStats{
		otherKeys: len(otherKeys), packageKeys: len(packageKeys), goKeys: len(goKeys),
	}
	loaded, err := s.loadCodeCallSymbolDefinitionLegs(ctx, packageKeys, goKeys, otherKeys, &load)
	load.log(ctx, err)
	return loaded, err
}

// loadCodeCallSymbolDefinitionLegs runs the three scans (keys no manifest
// anchors, package keys, Go keys) and merges their facts, recording each leg's
// producer scope count and duration in load.
func (s FactStore) loadCodeCallSymbolDefinitionLegs(
	ctx context.Context,
	packageKeys, goKeys, otherKeys []string,
	load *codeCallSymbolLoadStats,
) ([]facts.Envelope, error) {
	producers := producerstore.New(s.database).WithInstruments(s.instruments)

	var loaded []facts.Envelope
	if len(otherKeys) > 0 {
		started := time.Now()
		page, err := s.loadActiveCodeCallSymbolDefinitionFacts(
			ctx, listActiveCodeCallSymbolDefinitionFactsQuery, otherKeys, nil,
		)
		if err != nil {
			return nil, err
		}
		load.otherScanSeconds = time.Since(started).Seconds()
		loaded = append(loaded, page...)
	}
	for _, leg := range []struct {
		keys       []string
		producers  func(context.Context, []string) ([]string, error)
		scopeCount *int
		seconds    *float64
	}{
		{packageKeys, producers.PackageScopeIDs, &load.packageProducerScopes, &load.packageSeconds},
		{goKeys, producers.GoModuleScopeIDs, &load.goProducerScopes, &load.goSeconds},
	} {
		if len(leg.keys) == 0 {
			continue
		}
		started := time.Now()
		producerScopeIDs, err := leg.producers(ctx, leg.keys)
		if err != nil {
			return nil, err
		}
		*leg.scopeCount = len(producerScopeIDs)
		if len(producerScopeIDs) > 0 {
			anchored, err := s.loadActiveCodeCallSymbolDefinitionFacts(
				ctx, listAnchoredActiveCodeCallSymbolDefinitionFactsQuery, leg.keys, producerScopeIDs,
			)
			if err != nil {
				return nil, err
			}
			loaded = appendUniqueFactEnvelopes(loaded, anchored)
		}
		*leg.seconds = time.Since(started).Seconds()
	}
	return loaded, nil
}

// codeCallSymbolLoadStats records how one definition load split its keys and
// what each leg cost, so an operator can tell from one log line which scan made
// a slow code call materialization slow. A producer scope count of zero with
// keys of that group means no stored manifest names a producer, so that leg
// issued no definition scan and its keys stay unresolved.
type codeCallSymbolLoadStats struct {
	otherKeys, packageKeys, goKeys              int
	packageProducerScopes, goProducerScopes     int
	otherScanSeconds, packageSeconds, goSeconds float64
}

// log emits the load summary at Info, on success and on failure. The loader has
// no scope id, so correlate with the "code call materialization completed" line
// of the same consumer by adjacency. On failure the durations and producer scope
// counts cover only the legs that finished, so read outcome first.
func (l codeCallSymbolLoadStats) log(ctx context.Context, err error) {
	attrs := []any{
		"outcome", "ok",
		"other_key_count", l.otherKeys,
		"package_key_count", l.packageKeys,
		"go_key_count", l.goKeys,
		"package_producer_scope_count", l.packageProducerScopes,
		"go_producer_scope_count", l.goProducerScopes,
		"other_scan_duration_seconds", l.otherScanSeconds,
		"package_leg_duration_seconds", l.packageSeconds,
		"go_leg_duration_seconds", l.goSeconds,
	}
	if err != nil {
		attrs[1] = "error"
		attrs = append(attrs, "error", err.Error())
	}
	slog.Default().InfoContext(ctx, "code call symbol definition load", attrs...)
}

// loadActiveCodeCallSymbolDefinitionFacts pages one definition scan to the
// end. A non-nil producerScopeIDs is bound as $5 of the anchored statement.
func (s FactStore) loadActiveCodeCallSymbolDefinitionFacts(
	ctx context.Context,
	query string,
	symbolKeys []string,
	producerScopeIDs []string,
) ([]facts.Envelope, error) {
	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActiveCodeCallSymbolDefinitionFactsPage(
			ctx,
			query,
			symbolKeys,
			producerScopeIDs,
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

func (s FactStore) listActiveCodeCallSymbolDefinitionFactsPage(
	ctx context.Context,
	query string,
	symbolKeys []string,
	producerScopeIDs []string,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	args := []any{symbolKeys, cursor, cursorFactID, listFactsByKindPageSize}
	if producerScopeIDs != nil {
		args = append(args, producerScopeIDs)
	}
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active code call symbol definition facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active code call symbol definition facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active code call symbol definition facts: %w", err)
	}

	return loaded, nil
}

// appendUniqueFactEnvelopes appends the envelopes from extra whose fact_id is
// not already in loaded. One file fact can define both a package key and a
// non-package key, and then both scans return it.
func appendUniqueFactEnvelopes(loaded, extra []facts.Envelope) []facts.Envelope {
	if len(loaded) == 0 {
		return extra
	}
	seen := make(map[string]struct{}, len(loaded))
	for _, envelope := range loaded {
		seen[envelope.FactID] = struct{}{}
	}
	for _, envelope := range extra {
		if _, ok := seen[envelope.FactID]; ok {
			continue
		}
		seen[envelope.FactID] = struct{}{}
		loaded = append(loaded, envelope)
	}
	return loaded
}
