// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// codeCallSymbolDefinitionFactsSelect is the shared head of the definition
// scans: active, non-tombstoned file facts of each scope's active generation.
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
          WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'functions') = 'array'
          THEN fact.payload->'parsed_file_data'->'functions'
          ELSE '[]'::jsonb
        END
      ) AS definition(item)
      UNION ALL
      SELECT definition.item
      FROM jsonb_array_elements(
        CASE
          WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'classes') = 'array'
          THEN fact.payload->'parsed_file_data'->'classes'
          ELSE '[]'::jsonb
        END
      ) AS definition(item)
      UNION ALL
      SELECT definition.item
      FROM jsonb_array_elements(
        CASE
          WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'structs') = 'array'
          THEN fact.payload->'parsed_file_data'->'structs'
          ELSE '[]'::jsonb
        END
      ) AS definition(item)
      UNION ALL
      SELECT definition.item
      FROM jsonb_array_elements(
        CASE
          WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'interfaces') = 'array'
          THEN fact.payload->'parsed_file_data'->'interfaces'
          ELSE '[]'::jsonb
        END
      ) AS definition(item)
      UNION ALL
      SELECT definition.item
      FROM jsonb_array_elements(
        CASE
          WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'type_aliases') = 'array'
          THEN fact.payload->'parsed_file_data'->'type_aliases'
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
// the corpus. It serves keys that do not name a package (Go stable_symbol_key,
// SCIP symbols), whose producer the loader cannot resolve up front.
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
// Keys of the form package:<package_id>#<export_name> are anchored on their
// producer scopes: the repositories whose stored package.json manifests name
// that package. Only those scopes' active file facts are scanned, and a
// package key with no producer issues no definition scan and stays
// unresolved. Every other key keeps the corpus-wide scan.
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

	packageKeys, otherKeys := splitCodeCallPackageSymbolKeys(symbolKeys)

	var loaded []facts.Envelope
	if len(otherKeys) > 0 {
		page, err := s.loadActiveCodeCallSymbolDefinitionFacts(
			ctx, listActiveCodeCallSymbolDefinitionFactsQuery, otherKeys, nil,
		)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
	}
	if len(packageKeys) == 0 {
		return loaded, nil
	}

	producerScopeIDs, err := s.listCodeCallPackageProducerScopeIDs(ctx, packageKeys)
	if err != nil {
		return nil, err
	}
	if len(producerScopeIDs) == 0 {
		return loaded, nil
	}
	anchored, err := s.loadActiveCodeCallSymbolDefinitionFacts(
		ctx, listAnchoredActiveCodeCallSymbolDefinitionFactsQuery, packageKeys, producerScopeIDs,
	)
	if err != nil {
		return nil, err
	}
	return appendUniqueFactEnvelopes(loaded, anchored), nil
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

// codeCallPackageSymbolKeyPrefix starts the import-binding key the reducer
// derives from a definition's package_id and export_name
// (package:<package_id>#<export_name>).
const codeCallPackageSymbolKeyPrefix = "package:"

// listActiveCodeCallPackageManifestsQuery reads every stored package.json
// manifest of a repository scope that has an active generation. It reads all
// manifests, nested workspace packages included, because a package name maps
// to its repository wherever its manifest sits; the nearest-manifest rule
// applies when the parser stamps package_id on a definition, not here.
//
// content_files holds the latest projected content of each repository and has
// no generation column. The definition scan that follows reads only active
// generation file facts and still matches each definition's own package_id, so
// a manifest that is ahead of or behind the active generation can only add or
// drop a candidate scope.
//
// The manifest read is a MATERIALIZED CTE on purpose. Inlined, the planner
// estimates the scope join at one row and probes content_files once per
// repository scope; on the ops-qa replica (2026-10-04) that took 125 ms warm
// and 758 ms cold over 155k buffers. Materialized, content_files is read once
// through content_files_relative_path_trgm_idx: 15 to 21 ms and 3,497 buffers
// for the same 469 rows. See
// docs/internal/evidence/7601-anchored-symbol-definition-loader.md.
const listActiveCodeCallPackageManifestsQuery = `
WITH manifest AS MATERIALIZED (
    SELECT repo_id, content
    FROM content_files
    WHERE relative_path = 'package.json'
       OR relative_path LIKE '%/package.json'
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

// splitCodeCallPackageSymbolKeys separates package:<package_id>#<export_name>
// keys from every other symbol key, keeping the input order of each group.
func splitCodeCallPackageSymbolKeys(symbolKeys []string) (packageKeys, otherKeys []string) {
	for _, key := range symbolKeys {
		if strings.HasPrefix(key, codeCallPackageSymbolKeyPrefix) {
			packageKeys = append(packageKeys, key)
			continue
		}
		otherKeys = append(otherKeys, key)
	}
	return packageKeys, otherKeys
}

// codeCallPackageSymbolKeyPackageName returns the package_id of a
// package:<package_id>#<export_name> key, trimmed to match the trimmed manifest
// names, or "" when either part is empty.
// npm package names cannot contain '#', so the first '#' ends the name.
// Every key with the package: prefix takes the anchored path, so a key in any
// other shape (no '#', an empty part) resolves no producer and stays
// unresolved. No emitter produces such keys today; a new package: key shape
// must change this function too.
func codeCallPackageSymbolKeyPackageName(key string) string {
	packageName, exportName, ok := strings.Cut(strings.TrimPrefix(key, codeCallPackageSymbolKeyPrefix), "#")
	if !ok || strings.TrimSpace(packageName) == "" || strings.TrimSpace(exportName) == "" {
		return ""
	}
	return strings.TrimSpace(packageName)
}

// codeCallPackageManifestName returns the "name" of a package.json manifest.
// Invalid JSON, a non-object document, or a missing, blank, or non-string
// name yields "" so one bad manifest never fails the load.
func codeCallPackageManifestName(content string) string {
	var manifest struct {
		Name any `json:"name"`
	}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return ""
	}
	name, _ := manifest.Name.(string)
	return strings.TrimSpace(name)
}

// listCodeCallPackageProducerScopeIDs resolves the package keys to the sorted,
// distinct scope ids whose stored manifests publish one of the named packages.
// A package published by several repositories returns every one of them, so
// the reducer sees each candidate definition and keeps the key unresolved.
func (s FactStore) listCodeCallPackageProducerScopeIDs(
	ctx context.Context,
	packageKeys []string,
) ([]string, error) {
	packageNames := make(map[string]struct{}, len(packageKeys))
	for _, key := range packageKeys {
		if name := codeCallPackageSymbolKeyPackageName(key); name != "" {
			packageNames[name] = struct{}{}
		}
	}
	if len(packageNames) == 0 {
		return nil, nil
	}

	rows, err := s.database.QueryContext(ctx, listActiveCodeCallPackageManifestsQuery)
	if err != nil {
		return nil, fmt.Errorf("list code call package producer manifests: %w", err)
	}
	defer func() { _ = rows.Close() }()

	scopeIDs := make([]string, 0)
	for rows.Next() {
		var scopeID, content string
		if err := rows.Scan(&scopeID, &content); err != nil {
			return nil, fmt.Errorf("list code call package producer manifests: %w", err)
		}
		if _, ok := packageNames[codeCallPackageManifestName(content)]; ok {
			scopeIDs = append(scopeIDs, scopeID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list code call package producer manifests: %w", err)
	}

	slices.Sort(scopeIDs)
	return slices.Compact(scopeIDs), nil
}
