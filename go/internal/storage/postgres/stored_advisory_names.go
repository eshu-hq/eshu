// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// storedAdvisoryNameChunkSize bounds the advisory ids one statement carries.
// The advisory lookup index answers one index search per id and per
// (scope, generation) pair, so statement cost follows the id count; a burst of
// changed ids runs as several short statements instead of one long one. The
// size is a planning bound only: the result is the union over chunks and does
// not depend on it.
const storedAdvisoryNameChunkSize = 500

// storedAdvisoryNamesQuery reads the (ecosystem, package_name, package_id)
// rows Eshu stores for a set of advisory ids, from active generations only
// (#9777).
//
// The candidates CTE is the anchor and is MATERIALIZED on purpose. Its
// fact_kind list is byte-for-byte the partial predicate of
// fact_records_vulnerability_active_advisory_lookup_v2_idx (a test derives it
// from the index DDL) and its id expression is the index key, so that index is
// the only partial index the predicate can use: a bare
// fact_kind = 'vulnerability.affected_package' also admits the purl and
// active-scan partial indexes, and the planner took the purl index with a full
// filtered scan on the QA replica (547 ms for 1,180 ids). The kind is narrowed
// to affected packages after the index read; the other kinds the index holds
// for an advisory id add a handful of rows. The MATERIALIZED fence keeps the
// planner from joining the active scopes first: it estimated one row and
// probed once per active scope (1,933 probes, 1,021 ms cold), so cost followed
// the number of scopes instead of the number of ids
// (docs/internal/evidence/9777-stored-advisory-names.md). The active-generation
// joins, the ones every active reader uses (the scope's active_generation_id
// and an active scope_generations row), then run on the candidate rows.
// The read is a name selector, not an evidence read: an over-matched name costs
// one extra re-query, so it does not apply the advisory-authority receipt chain
// that the impact loader applies to findings. Bind $1: advisory ids.
const storedAdvisoryNamesQuery = `
WITH candidates AS MATERIALIZED (
  SELECT
    fact.payload->>'advisory_id' AS advisory_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    LOWER((BTRIM(COALESCE(fact.payload->>'ecosystem', ''))) COLLATE "C") COLLATE "default" AS ecosystem,
    BTRIM(COALESCE(fact.payload->>'package_name', '')) AS package_name,
    BTRIM(COALESCE(fact.payload->>'package_id', '')) AS package_id
  FROM fact_records AS fact
  WHERE fact.fact_kind IN ('vulnerability.cve', 'vulnerability.affected_package', 'vulnerability.affected_product', 'vulnerability.epss_score', 'vulnerability.known_exploited', 'vulnerability.reference')
    AND fact.is_tombstone = FALSE
    AND fact.payload->>'advisory_id' = ANY($1::text[])
)
SELECT DISTINCT
    candidate.advisory_id,
    candidate.ecosystem,
    candidate.package_name,
    candidate.package_id
FROM candidates AS candidate
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidate.scope_id
 AND scope.active_generation_id = candidate.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = candidate.scope_id
 AND generation.generation_id = candidate.generation_id
WHERE generation.status = 'active'
  AND candidate.fact_kind = 'vulnerability.affected_package'
  AND candidate.ecosystem <> ''
  AND candidate.package_name <> ''
`

// StoredAdvisoryPackageName is one package Eshu holds for an advisory id in an
// active generation, as the collector wrote it. All three values are trimmed.
//
// Ecosystem is the lowercased packageidentity canonical ecosystem, not the
// owned-package derivation spelling: Go is "gomod" here and "go" there. A
// caller that compares with the derivation maps it with
// workflow.CanonicalOwnedPackageEcosystem.
//
// PackageName is the packageidentity normalized name. For Maven and Swift it is
// not the name a consumer manifest declares: both keep only the last part, the
// Maven group and the Swift namespace live in PackageID, and two Maven packages
// can share one PackageName. Owned-package keys are hashed over the manifest
// dependency's name, so a caller rebuilds that name (group:artifact for Maven,
// the repository path for Swift) from PackageID instead of using this value.
// PyPI is already folded. Hex is the exception to the rebuild: its manifest
// name is the bare package, which is PackageName, and its PackageID registry is
// the placeholder "unknown", so a caller must not parse a Hex PackageID for a
// name.
//
// PackageID is the payload's canonical package identity, for example
// maven://repo.maven.apache.org/maven2/org.example:lib. It is empty when the
// stored row has none.
type StoredAdvisoryPackageName struct {
	Ecosystem   string
	PackageName string
	PackageID   string
}

// ListStoredAdvisoryPackageNames returns, per advisory id, the sorted and
// de-duplicated packages Eshu stores for it in active generations. The change
// feed reader unions these with the names a changed OSV record carries now, so
// a record that dropped a name (6 of 768 modified records in the #9777 replay)
// still re-queries the old name. An id with no stored package is absent from
// the map: Eshu stores facts only for advisories that matched, and an id with
// no facts cannot leave a stale finding. Empty and repeated ids are ignored, and
// a nil or empty input returns an empty map without touching the database. The
// id list runs in chunks of storedAdvisoryNameChunkSize.
//
// Ids match the stored advisory_id exactly and case-sensitively, so callers
// pass them as OSV spells them. OSV facts store the record id, so a changed
// record finds its facts, but an id that is only an alias of a record (a CVE or
// GHSA id) finds nothing. The read has no source filter: another producer that
// writes the same advisory_id adds its packages to the result, which an
// over-matching selector accepts.
func (s FactStore) ListStoredAdvisoryPackageNames(
	ctx context.Context,
	advisoryIDs []string,
) (map[string][]StoredAdvisoryPackageName, error) {
	ids := cleanStringFilterValues(advisoryIDs)
	if len(ids) == 0 {
		return map[string][]StoredAdvisoryPackageName{}, nil
	}
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(ctx, contract.SpanStoredAdvisoryNamesRead)
	defer span.End()

	names := make(map[string][]StoredAdvisoryPackageName)
	chunks := 0
	for chunk := range slices.Chunk(ids, storedAdvisoryNameChunkSize) {
		chunks++
		if err := s.readStoredAdvisoryNameChunk(ctx, chunk, names); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "stored advisory name read failed")
			return nil, err
		}
	}
	packages := 0
	for _, list := range names {
		packages += len(list)
	}
	span.SetAttributes(
		attribute.Int("stored_advisory_names.ids_requested", len(ids)),
		attribute.Int("stored_advisory_names.chunks", chunks),
		attribute.Int("stored_advisory_names.ids_resolved", len(names)),
		attribute.Int("stored_advisory_names.packages", packages),
	)
	return names, nil
}

// readStoredAdvisoryNameChunk runs the statement for one chunk of ids and
// merges the packages into names. Each row arrives once (SELECT DISTINCT) and
// each id sits in exactly one chunk, so a merge never sees a duplicate; the
// per-id lists are sorted once the chunk is read.
func (s FactStore) readStoredAdvisoryNameChunk(
	ctx context.Context,
	ids []string,
	names map[string][]StoredAdvisoryPackageName,
) error {
	rows, err := s.database.QueryContext(ctx, storedAdvisoryNamesQuery, ids)
	if err != nil {
		return fmt.Errorf("list stored advisory package names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	touched := make(map[string]struct{})
	for rows.Next() {
		var advisoryID, ecosystem, packageName, packageID string
		if err := rows.Scan(&advisoryID, &ecosystem, &packageName, &packageID); err != nil {
			return fmt.Errorf("list stored advisory package names: %w", err)
		}
		names[advisoryID] = append(names[advisoryID], StoredAdvisoryPackageName{
			Ecosystem:   ecosystem,
			PackageName: packageName,
			PackageID:   packageID,
		})
		touched[advisoryID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list stored advisory package names: %w", err)
	}
	for id := range touched {
		slices.SortFunc(names[id], func(a, b StoredAdvisoryPackageName) int {
			if c := strings.Compare(a.Ecosystem, b.Ecosystem); c != 0 {
				return c
			}
			if c := strings.Compare(a.PackageName, b.PackageName); c != 0 {
				return c
			}
			return strings.Compare(a.PackageID, b.PackageID)
		})
	}
	return nil
}
