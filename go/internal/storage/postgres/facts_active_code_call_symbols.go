// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/filters"
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
// file facts instead of the whole corpus (#7601). That cost promise holds
// for anchored legs, whose $5 is the producer set; base-only union legs
// (#10104) bind the baselined delta-active targets instead, so their width
// scales with the delta-active scope count (read it off the union scope
// counters on the load log line).
const listAnchoredActiveCodeCallSymbolDefinitionFactsQuery = codeCallSymbolDefinitionFactsSelect +
	"  AND fact.scope_id = ANY($5::text[])\n" +
	codeCallSymbolDefinitionFactsMatch

// The candidate base generation ids bind at $5 of the derived unanchored
// base text ($1-$4 are keys, cursor, page size) and at $6 of the anchored
// base text ($5 is the producer scopes for anchored legs, or the baselined
// delta-active targets' scope ids for union legs, #10104).
const (
	codeCallSymbolDeltaOverlayScopeParam         = 5
	codeCallAnchoredSymbolDeltaOverlayScopeParam = 6
)

// codeCallSymbolDeltaOverlayTelemetry names where the load records its
// delta-overlay decisions (#10003 row 1):
// eshu_dp_code_call_symbol_definition_read_delta_union_total by read and
// outcome, the per-read span event, and WARN lines.
func (s FactStore) codeCallSymbolDeltaOverlayTelemetry() deltaOverlayTelemetry {
	sink := deltaOverlayTelemetry{
		read:      "code_call_symbol_definitions",
		subject:   "code call symbol definition load",
		spanEvent: "code_call_symbol_definition_read.delta_union",
	}
	if s.instruments != nil {
		sink.counter = s.instruments.CodeCallSymbolDefinitionReadDeltaUnion
	}
	return sink
}

// LoadActiveCodeCallSymbolDefinitionFacts loads active file facts whose parsed
// definitions carry one of the requested stable symbol keys.
//
// Two kinds of key are anchored on the repositories that can define them, so
// the scan reads only those repositories' active file facts:
//
//   - package:<package_id>#<export_name> keys, on the repositories whose stored
//     package.json manifests name that npm package, or whose composer.json
//     manifests name that composer package (#7601);
//   - scip-go gomod <import path> <symbol> keys, on the repositories whose
//     stored go.mod module path is that import path or a prefix of it (#7623).
//     The Go parser builds a definition's import path from its nearest go.mod
//     module path, so the defining repository always stores such a go.mod.
//
// A key with no producer issues no active scan. With no delta-active scope
// carrying a baseline it stays unresolved; otherwise its group runs a
// base-only union leg over the baselined targets' scopes, and the match on
// each definition's own package id gates the wider base scopes (#10104).
// Every other key keeps the corpus-wide scan, and the groups run separately
// so one group's manifest never widens another.
//
// A scope whose active generation is a git delta reads its delta overlaid on
// its #7797 base full generation (#10003 row 1); see overlayDeltaBase for
// the win and refusal rules. Every statement runs on one read-only
// repeatable-read snapshot. The leg bindings live in
// active_code_call_symbol_legs.go.
func (s FactStore) LoadActiveCodeCallSymbolDefinitionFacts(
	ctx context.Context,
	symbolKeys []string,
) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	symbolKeys = filters.CleanFactKinds(symbolKeys)
	if len(symbolKeys) == 0 {
		return nil, nil
	}

	packageKeys, goKeys, otherKeys := producerstore.Split(symbolKeys)
	load := codeCallSymbolLoadStats{
		otherKeys: len(otherKeys), packageKeys: len(packageKeys), goKeys: len(goKeys),
	}
	if len(packageKeys) == 0 && len(goKeys) == 0 && len(otherKeys) == 0 {
		return nil, nil
	}
	unanchoredBase, err := deriveDeltaOverlayBaseQuery(
		listActiveCodeCallSymbolDefinitionFactsQuery, codeCallSymbolDeltaOverlayScopeParam,
	)
	if err != nil {
		load.log(ctx, err)
		return nil, err
	}
	anchoredBase, err := deriveDeltaOverlayBaseQuery(
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery, codeCallAnchoredSymbolDeltaOverlayScopeParam,
	)
	if err != nil {
		load.log(ctx, err)
		return nil, err
	}
	sink := s.codeCallSymbolDeltaOverlayTelemetry()
	var loaded []facts.Envelope
	err = s.withDeltaOverlaySnapshot(ctx, func(snapshot *deltaOverlaySnapshot) error {
		reader := snapshot.store
		active, base, legsErr := reader.loadCodeCallSymbolDefinitionLegRows(
			ctx, snapshot, packageKeys, goKeys, otherKeys, unanchoredBase, anchoredBase, sink, &load,
		)
		if legsErr != nil {
			return legsErr
		}
		// deltaTargets is cached on the snapshot: the leg pass already read
		// it for the base pages, so this re-read issues no statement.
		targets, targetsErr := snapshot.deltaTargets(ctx)
		if targetsErr != nil {
			reader.recordDeltaOverlayFailure(ctx, sink)
			return targetsErr
		}
		overlaid, overlayErr := reader.overlayDeltaBase(ctx, sink, targets, active, base)
		if overlayErr != nil {
			return overlayErr
		}
		loaded = overlaid
		return nil
	})
	load.log(ctx, err)
	return loaded, err
}

// loadCodeCallSymbolDefinitionLegRows runs the three scans and returns the
// active rows and the base candidates separately for the overlay, recording
// producer scope counts and leg durations in load. Active pages, the
// delta-target lookup, then base pages run in that order on the one
// snapshot; any failure aborts the read. The base pages run only when a
// delta-active scope with a recorded baseline exists; an anchored group with
// keys but no producer then contributes a base-only union leg (see
// active_code_call_symbol_legs.go, #10104).
func (s FactStore) loadCodeCallSymbolDefinitionLegRows(
	ctx context.Context,
	snapshot *deltaOverlaySnapshot,
	packageKeys, goKeys, otherKeys []string,
	unanchoredBase, anchoredBase string,
	sink deltaOverlayTelemetry,
	load *codeCallSymbolLoadStats,
) (active, base []facts.Envelope, err error) {
	producers := producerstore.New(s.database).WithInstruments(s.instruments)

	// The statement order matches the pre-overlay read exactly (unanchored
	// scan, then per anchored leg its producer discovery and scan), so the
	// only new statements are the overlay's own: the delta-target lookup
	// after the active pages and the base pages after that.
	var legs []codeCallSymbolDefinitionLeg
	var emptyAnchored []codeCallSymbolAnchoredUnion
	var activeRows []facts.Envelope
	if len(otherKeys) > 0 {
		started := time.Now()
		page, err := s.loadActiveCodeCallSymbolDefinitionFacts(
			ctx, listActiveCodeCallSymbolDefinitionFactsQuery, otherKeys, nil, nil,
		)
		if err != nil {
			return nil, nil, err
		}
		activeRows = append(activeRows, page...)
		load.otherScanSeconds = time.Since(started).Seconds()
		legs = append(legs, codeCallSymbolDefinitionLeg{
			keys:        otherKeys,
			activeQuery: listActiveCodeCallSymbolDefinitionFactsQuery,
			baseQuery:   unanchoredBase,
			seconds:     &load.otherScanSeconds,
		})
	}
	for _, leg := range []struct {
		keys       []string
		producers  func(context.Context, []string) ([]string, error)
		scopeCount *int
		seconds    *float64
		unionCount *int
	}{
		{packageKeys, producers.PackageScopeIDs, &load.packageProducerScopes, &load.packageSeconds, &load.packageUnionScopes},
		{goKeys, producers.GoModuleScopeIDs, &load.goProducerScopes, &load.goSeconds, &load.goUnionScopes},
	} {
		if len(leg.keys) == 0 {
			continue
		}
		started := time.Now()
		producerScopeIDs, err := leg.producers(ctx, leg.keys)
		if err != nil {
			return nil, nil, err
		}
		*leg.scopeCount = len(producerScopeIDs)
		if len(producerScopeIDs) > 0 {
			anchored, err := s.loadActiveCodeCallSymbolDefinitionFacts(
				ctx, listAnchoredActiveCodeCallSymbolDefinitionFactsQuery, leg.keys, producerScopeIDs, nil,
			)
			if err != nil {
				return nil, nil, err
			}
			activeRows = appendUniqueFactEnvelopes(activeRows, anchored)
			legs = append(legs, codeCallSymbolDefinitionLeg{
				keys:             leg.keys,
				activeQuery:      listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
				baseQuery:        anchoredBase,
				producerScopeIDs: producerScopeIDs,
				seconds:          leg.seconds,
			})
		} else {
			// Empty discovery issues no active scan. A delta that rewrote
			// the producer manifest can still hold this group's base
			// definitions, so remember it for the union leg below (#10104).
			emptyAnchored = append(emptyAnchored, codeCallSymbolAnchoredUnion{
				keys:       leg.keys,
				seconds:    leg.seconds,
				unionCount: leg.unionCount,
			})
		}
		*leg.seconds = time.Since(started).Seconds()
	}
	targets, err := snapshot.deltaTargets(ctx)
	if err != nil {
		s.recordDeltaOverlayFailure(ctx, sink)
		return nil, nil, err
	}
	var baseRows []facts.Envelope
	if baseGenerations, runBase := deltaOverlayBaseGenerations(targets); runBase {
		// Base-only union legs (#10104): per anchored group with keys but
		// no producer, the base pass binds the baselined targets' scope
		// ids at $5 and the candidate base generation ids at $6 — split
		// domains, never the same set (#9611).
		var unionScopes []string
		for _, target := range targets {
			if target.commits.Baseline == "" {
				continue
			}
			unionScopes = append(unionScopes, target.scopeID)
		}
		slices.Sort(unionScopes)
		unionScopes = slices.Compact(unionScopes)
		legs = appendCodeCallSymbolUnionLegs(legs, emptyAnchored, unionScopes, anchoredBase)
		for i := range legs {
			started := time.Now()
			page, err := s.loadActiveCodeCallSymbolDefinitionFacts(
				ctx, legs[i].baseQuery, legs[i].keys, legs[i].producerScopeIDs, baseGenerations,
			)
			if err != nil {
				s.recordDeltaOverlayFailure(ctx, sink)
				return nil, nil, err
			}
			baseRows = appendUniqueFactEnvelopes(baseRows, page)
			*legs[i].seconds += time.Since(started).Seconds()
		}
	}
	return activeRows, baseRows, nil
}

// loadActiveCodeCallSymbolDefinitionFacts pages one definition scan to the
// end. A non-nil producerScopeIDs is bound as $5 of the anchored statement;
// a non-nil baseGenerationIDs is bound as $5 of the unanchored base text or $6 of
// the anchored base text.
func (s FactStore) loadActiveCodeCallSymbolDefinitionFacts(
	ctx context.Context,
	query string,
	symbolKeys []string,
	producerScopeIDs []string,
	baseGenerationIDs []string,
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
			baseGenerationIDs,
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
	baseGenerationIDs []string,
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
	if baseGenerationIDs != nil {
		args = append(args, baseGenerationIDs)
	}
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active code call symbol definition facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
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
