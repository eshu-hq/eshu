// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

// The retracted-keys table (#10880 PR-C1 c, migration 351) holds what a retraction
// removed from a (scope, generation): the CVE id, advisory id and advisory-group
// member set of each retracted finding. The recovery reads need those keys after a
// crashed attempt, and until now found them as tombstoned fact rows; PR-C stops
// tombstoning facts, so the retraction records them here, in the same statement
// and transaction as the supports delete, and the recovery reads take them from here.

// retractedFindingKeysRecordSQL is the data-modifying CTE both retraction
// statements run after their dirty staging: one row per distinct retracted key
// of the (scope $1, generation $2) pair. The key hash is a sha256 of the three
// key columns, so the primary key stays small. A key already recorded for the
// pair is skipped with NOT EXISTS, so a replayed or re-retracted finding adds
// nothing. There is no ON CONFLICT: the retraction statements keep no conflict
// arbiter on their hot path (TestRetractionCapturesRetractedFindingIdentifiers),
// and none is needed, because every statement that writes this pair's keys runs
// under the pair's conflict-domain lock, so two inserters of one key cannot race;
// DISTINCT keeps one statement from inserting a key twice.
const retractedFindingKeysRecordSQL = `,
recorded AS (
    INSERT INTO supply_chain_impact_retracted_finding_keys
        (generation_id, scope_id, key_hash, cve_id, advisory_id, members)
    SELECT DISTINCT $2::text, $1::text, hashed.key_hash, hashed.cve_id, hashed.advisory_id, hashed.members
    FROM (
        SELECT encode(sha256(convert_to(cve_id || E'\x1f' || advisory_id || E'\x1f' || members, 'UTF8')), 'hex') AS key_hash,
               cve_id, advisory_id, members
        FROM retracted
    ) AS hashed
    WHERE NOT EXISTS (
        SELECT 1
        FROM supply_chain_impact_retracted_finding_keys AS known
        WHERE known.generation_id = $2::text
          AND known.scope_id = $1::text
          AND known.key_hash = hashed.key_hash
    )
    RETURNING 1
)`

// listSupplyChainImpactCVEIDsQuery lists every CVE id the finding rows of the
// generation being written and of the previous generation name, plus each row's
// advisory group member set (#10408). It runs only for a recovery write: a retry
// cannot know what the attempt that died retracted or what the new generation
// dropped. Three legs joined by UNION (a set): the live supports, the retracted
// keys, and the finding facts with tombstones, the transition leg for rows
// retracted before the retracted-keys table existed
// (listSupplyChainImpactCVEIDsFactsLegQuery, byte-identical to the previous
// statement). Arguments: $1 scope id, $2 generation id.
const listSupplyChainImpactCVEIDsQuery = `
SELECT s.cve_id, COALESCE(s.payload #>> '{provenance,advisory_group_members}', '[]')
FROM supply_chain_impact_finding_supports AS s
WHERE s.scope_id = $1
  AND s.generation_id IN ($2, ` + priorGenerationSupplyChainImpactSQL + `)
UNION
SELECT k.cve_id, k.members
FROM supply_chain_impact_retracted_finding_keys AS k
WHERE k.scope_id = $1
  AND k.generation_id IN ($2, ` + priorGenerationSupplyChainImpactSQL + `)
UNION` + listSupplyChainImpactCVEIDsFactsLegQuery

// listSupplyChainImpactGroupIDsQuery lists the (cve_id, advisory_id) pair of
// every finding row of the generation being written and of the previous
// generation, the recovery companion of listSupplyChainImpactCVEIDsQuery for the
// advisory-group projection (#10411). Same three legs, same arguments.
const listSupplyChainImpactGroupIDsQuery = `
SELECT s.cve_id, s.advisory_id
FROM supply_chain_impact_finding_supports AS s
WHERE s.scope_id = $1
  AND s.generation_id IN ($2, ` + priorGenerationSupplyChainImpactSQL + `)
UNION
SELECT k.cve_id, k.advisory_id
FROM supply_chain_impact_retracted_finding_keys AS k
WHERE k.scope_id = $1
  AND k.generation_id IN ($2, ` + priorGenerationSupplyChainImpactSQL + `)
UNION` + listSupplyChainImpactGroupIDsFactsLegQuery
