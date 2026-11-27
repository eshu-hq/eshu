// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// The key_presence fill walks the supports table and sets the key-presence
// bitmask of every row the writer did not fill, in resumable steps (#9975
// round 6). It is the stable-key fill's twin: one marker row in the same
// backfill table, a resumable (scope, generation, fact) cursor advanced in
// the chunk's own transaction, chunks under the pair's conflict-domain lock,
// completion only after a sliced disagreement-0 verification. The marker,
// ensure, read, advance, restart and verification-cursor statements are the
// supports backfill's (they act on a marker by name); the walk, the fill and
// the verification below are this job's.
//
// Unlike the stable-key fill, this fill's marker gates a reader: the keyed
// tick runs the triple-guarded probe only after this marker completes, so the
// probe never sees a fail-open row. The marker completes only on a clean
// verification, which restarts the sweep on any disagreement — including rows
// an old binary wrote with DEFAULT 0 bits — so the flip waits for the
// rollout to drain old writers the same way every marker-gated cutover does.

const (
	// SupplyChainImpactKeyPresenceFillMarker names the fill's marker row in
	// supply_chain_impact_finding_supports_backfill. It is a second row beside
	// the supports and stable-key markers and never touches them.
	SupplyChainImpactKeyPresenceFillMarker = "supply_chain_impact_key_presence_fill_v1"

	// SupplyChainImpactKeyPresenceFillContractVersion is the contract_version the
	// fill's marker row carries. It must never exceed
	// SupplyChainImpactSupportsContractVersion: the readiness predicate and the
	// readers refuse on any marker row whose contract_version is above their own
	// (supplyChainImpactSupportsReadySQL, "contract_version > $2"), so a fill marker
	// above the supports version would turn every supports reader to 503. A test pins
	// the bound.
	SupplyChainImpactKeyPresenceFillContractVersion = 1

	// supplyChainImpactKeyPresenceFillMaxChunk is the hard ceiling of rows one fill
	// transaction updates under one conflict-domain lock, whatever a caller asks for.
	supplyChainImpactKeyPresenceFillMaxChunk = 500

	// supplyChainImpactKeyPresenceFillMaxPairsPerStep bounds how many pairs without
	// a fillable row one step walks, so a step over already-filled pairs stays
	// short and the cursor still advances.
	supplyChainImpactKeyPresenceFillMaxPairsPerStep = 200
)

// supplyChainImpactKeyPresenceFillNextPairSQL finds the next (scope, generation)
// holding a support after the pair ($1, $2), in (scope, generation) order, through
// the supports table's (scope_id, generation_id) index.
var supplyChainImpactKeyPresenceFillNextPairSQL = `
SELECT scope_id, generation_id
FROM supply_chain_impact_finding_supports
WHERE (scope_id, generation_id) > ($1::text, $2::text)
ORDER BY scope_id, generation_id
LIMIT 1`

// supplyChainImpactKeyPresenceFillChunkSQL lists up to $4 supports of the pair
// ($1, $2) whose stored bits disagree with their payload after fact id $3, in
// fact id order.
var supplyChainImpactKeyPresenceFillChunkSQL = `
SELECT fact_id
FROM supply_chain_impact_finding_supports AS s
WHERE scope_id = $1 AND generation_id = $2
  AND s.key_presence IS DISTINCT FROM ` + supplyChainImpactKeyPresenceFillExpr("s") + `
  AND fact_id > $3
ORDER BY fact_id
LIMIT $4`

// supplyChainImpactKeyPresenceFillUpdateSQL sets the payload-derived bits on the
// supports of $1 that still disagree. It touches only key_presence and
// rechecks the disagreement, so a writer's concurrent fill of the same rows
// (the writer upsert sets the same expression) converges instead of
// conflicting.
var supplyChainImpactKeyPresenceFillUpdateSQL = `
UPDATE supply_chain_impact_finding_supports AS s
SET key_presence = ` + supplyChainImpactKeyPresenceFillExpr("s") + `
WHERE s.fact_id = ANY($1::text[])
  AND s.key_presence IS DISTINCT FROM ` + supplyChainImpactKeyPresenceFillExpr("s")

// supplyChainImpactKeyPresenceFillVerifySliceSQL checks one bounded interval of
// the verification in one snapshot: up to $4 supports after the cursor
// ($1, $2, $3) in (scope_id, generation_id, fact_id) order, and how many of them
// still disagree with their payload. The slice is the last when fewer than $4
// supports came back. The bound columns are the last key read.
var supplyChainImpactKeyPresenceFillVerifySliceSQL = `
WITH slice AS MATERIALIZED (
    SELECT s.scope_id, s.generation_id, s.fact_id, s.key_presence, s.payload
    FROM supply_chain_impact_finding_supports AS s
    WHERE (s.scope_id, s.generation_id) >= ($1::text, $2::text)
      AND (s.scope_id, s.generation_id, s.fact_id) > ($1::text, $2::text, $3::text)
    ORDER BY s.scope_id, s.generation_id, s.fact_id
    LIMIT $4::bigint
)
SELECT (SELECT count(*) FROM slice),
       (SELECT count(*) FROM slice AS x
        WHERE x.key_presence IS DISTINCT FROM ` + supplyChainImpactKeyPresenceFillExpr("x") + `),
       (SELECT count(*) FROM slice) < $4::bigint,
       COALESCE((SELECT scope_id FROM slice ORDER BY scope_id DESC, generation_id DESC, fact_id DESC LIMIT 1), ''),
       COALESCE((SELECT generation_id FROM slice ORDER BY scope_id DESC, generation_id DESC, fact_id DESC LIMIT 1), ''),
       COALESCE((SELECT fact_id FROM slice ORDER BY scope_id DESC, generation_id DESC, fact_id DESC LIMIT 1), '')`

// supplyChainImpactKeyPresenceFillCompleteSQL sets the fill's completion stamp.
// Like the stable-key fill's it has no old-writer fence: the probe flip reads
// this marker, and a support written by an old writer without bits fails the
// verification and restarts the sweep (or the rollout drains the writer),
// so completion itself is the proof the table agrees.
var supplyChainImpactKeyPresenceFillCompleteSQL = `
UPDATE supply_chain_impact_finding_supports_backfill
SET completed_at = now(), updated_at = now()
WHERE marker_name = $1 AND completed_at IS NULL`

// supplyChainImpactKeyPresenceFillExpr renders the key-presence bitmask over the
// relation named by alias: bit 1 when the payload carries package_id, 2 for
// repository_id, 4 for subject_digest. Presence is `->>` non-NULL, so an
// explicit ” counts as present while a missing key or JSON null does not.
// Advisory keys carry no bits: the keyed probe's c0/advisory legs are plain
// equalities with no guards. This is the single source of the expression the
// writer's projection (alias "fact"), the fill's chunk, update and
// verification render, so the writer and the backfill cannot disagree on a bit.
func supplyChainImpactKeyPresenceFillExpr(alias string) string {
	return `((CASE WHEN ` + alias + `.payload->>'package_id' IS NOT NULL THEN 1 ELSE 0 END) | (CASE WHEN ` + alias + `.payload->>'repository_id' IS NOT NULL THEN 2 ELSE 0 END) | (CASE WHEN ` + alias + `.payload->>'subject_digest' IS NOT NULL THEN 4 ELSE 0 END))`
}
