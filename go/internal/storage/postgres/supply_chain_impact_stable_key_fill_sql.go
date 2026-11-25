// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// The stable_fact_key fill walks the supports table and copies the key of each
// support's fact twin into the support, for the rows no rewrite has filled
// (#10880 PR-C1k, arbiter ruling C1 sections 1.4 to 1.7). It is the supports
// backfill's twin: one marker row in the same table, a resumable
// (scope, generation, fact) cursor advanced in the chunk's own transaction, chunks
// under the pair's conflict-domain advisory lock, completion only after a sliced
// verification. The marker, ensure, read, advance, restart and verification-cursor
// statements are the backfill's (they act on a marker by name); the walk, the fill
// and the verification below are this job's.

const (
	// SupplyChainImpactStableKeyFillMarker names the fill's marker row in
	// supply_chain_impact_finding_supports_backfill. It is a second row beside
	// SupplyChainImpactSupportsBackfillMarker and never touches it.
	SupplyChainImpactStableKeyFillMarker = "supply_chain_impact_stable_key_fill_v1"

	// SupplyChainImpactStableKeyFillContractVersion is the contract_version the
	// fill's marker row carries. It must never exceed
	// SupplyChainImpactSupportsContractVersion: the readiness predicate and the
	// readers refuse on any marker row whose contract_version is above their own
	// (supplyChainImpactSupportsReadySQL, "contract_version > $2"), so a fill marker
	// above the supports version would turn every supports reader to 503. A test pins
	// the bound.
	SupplyChainImpactStableKeyFillContractVersion = 1

	// supplyChainImpactStableKeyFillMaxChunk is the hard ceiling of rows one fill
	// transaction updates under one conflict-domain lock, whatever a caller asks for.
	supplyChainImpactStableKeyFillMaxChunk = 500

	// supplyChainImpactStableKeyFillMaxPairsPerStep bounds how many pairs without
	// a fillable row one step walks, so a step over already-filled pairs stays
	// short and the cursor still advances.
	supplyChainImpactStableKeyFillMaxPairsPerStep = 200
)

// supplyChainImpactStableKeyFillNextPairSQL finds the next (scope, generation)
// holding a support after the pair ($1, $2), in (scope, generation) order, through
// the supports table's (scope_id, generation_id) index.
const supplyChainImpactStableKeyFillNextPairSQL = `
SELECT scope_id, generation_id
FROM supply_chain_impact_finding_supports
WHERE (scope_id, generation_id) > ($1::text, $2::text)
ORDER BY scope_id, generation_id
LIMIT 1`

// supplyChainImpactStableKeyFillChunkSQL lists up to $4 supports of the pair
// ($1, $2) with no stable key after fact id $3, in fact id order.
const supplyChainImpactStableKeyFillChunkSQL = `
SELECT fact_id
FROM supply_chain_impact_finding_supports
WHERE scope_id = $1 AND generation_id = $2
  AND stable_fact_key IS NULL
  AND fact_id > $3
ORDER BY fact_id
LIMIT $4`

// supplyChainImpactStableKeyFillUpdateSQL copies the fact twin's key into the
// supports of $1 that still have none. It touches only stable_fact_key, never
// overwrites a key a rewrite already filled (IS NULL), and skips a twin with an
// empty key: the writer's reader treats an empty key as missing either way, and an
// empty key written here would only hide the support from the verification.
const supplyChainImpactStableKeyFillUpdateSQL = `
UPDATE supply_chain_impact_finding_supports AS s
SET stable_fact_key = f.stable_fact_key
FROM fact_records AS f
WHERE s.fact_id = ANY($1::text[])
  AND s.stable_fact_key IS NULL
  AND f.fact_id = s.fact_id
  AND f.stable_fact_key <> ''`

// supplyChainImpactStableKeyFillVerifySliceSQL checks one bounded interval of the
// verification in one snapshot: up to $4 supports after the cursor ($1, $2, $3) in
// (scope_id, generation_id, fact_id) order, and how many of them still have no key
// although their twin has one. The slice is the last when fewer than $4 supports
// came back. The bound columns are the last key read.
const supplyChainImpactStableKeyFillVerifySliceSQL = `
WITH slice AS MATERIALIZED (
    SELECT s.scope_id, s.generation_id, s.fact_id, s.stable_fact_key
    FROM supply_chain_impact_finding_supports AS s
    WHERE (s.scope_id, s.generation_id) >= ($1::text, $2::text)
      AND (s.scope_id, s.generation_id, s.fact_id) > ($1::text, $2::text, $3::text)
    ORDER BY s.scope_id, s.generation_id, s.fact_id
    LIMIT $4::bigint
)
SELECT (SELECT count(*) FROM slice),
       (SELECT count(*) FROM slice AS x
        WHERE x.stable_fact_key IS NULL
          AND EXISTS (SELECT 1 FROM fact_records AS f WHERE f.fact_id = x.fact_id AND f.stable_fact_key <> '')),
       (SELECT count(*) FROM slice) < $4::bigint,
       COALESCE((SELECT scope_id FROM slice ORDER BY scope_id DESC, generation_id DESC, fact_id DESC LIMIT 1), ''),
       COALESCE((SELECT generation_id FROM slice ORDER BY scope_id DESC, generation_id DESC, fact_id DESC LIMIT 1), ''),
       COALESCE((SELECT fact_id FROM slice ORDER BY scope_id DESC, generation_id DESC, fact_id DESC LIMIT 1), '')`

// supplyChainImpactStableKeyFillCompleteSQL sets the fill's completion stamp. Unlike
// the supports backfill's it has no old-writer fence: the fill's marker never gates
// a reader, and a support written by an old writer without a key is filled by the next
// sweep or by its own rewrite.
const supplyChainImpactStableKeyFillCompleteSQL = `
UPDATE supply_chain_impact_finding_supports_backfill
SET completed_at = now(), updated_at = now()
WHERE marker_name = $1 AND completed_at IS NULL`
