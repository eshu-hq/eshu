// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "strings"

// Apply of the keyed resweep (#9805): recompute the identifier set's winners
// over the keyed active_facts, write only new or changed rows, delete stored
// winners of the set that are no longer produced, and stamp the watermark.
// Moved verbatim from supply_chain_impact_winners_keyed_sql.go for the
// 500-line file cap; behavior-preserving move, no statement text changed.

// winnersStoredIdentMatch matches stored winner rows (alias w) to the
// identifier table; the winners table keeps the same coalesced columns.
const winnersStoredIdentMatch = `(w.package_id, w.repository_id, w.subject_digest,
        COALESCE(NULLIF(w.cve_id, ''), NULLIF(w.advisory_id, ''), ''), w.advisory_id)
       IN (SELECT package_id, repository_id, subject_digest, c0, advisory_id FROM ` + winnersKeyedIdentsTable + `)`

// applyWinnersKeyedSQL recomputes the identifier set's winners and writes only
// the rows that are new or changed, deletes stored winners of the identifier
// set that are no longer produced, and stamps the watermark. It is the keyed
// twin of rebuildSupplyChainImpactWinnersSQL: the same changed/upserted/deleted
// shape and the same row comparison, restricted to the identifier set. $1 is
// the stamp candidate: the DB-clock tick stamp S when the log drained, or
// the horizon H = min(captured_at) of the remainder minus 1us when rows are
// still queued (#10706). The stamp is GREATEST(previous, candidate), so the
// watermark never regresses. It returns inserted, updated, deleted,
// unchanged, the keys recomputed (winners now produced plus winners deleted),
// and the post-stamp watermark (scanned for arity only; the replaced value
// is pre-read by the caller, #11515).
var applyWinnersKeyedSQL = applyWinnersKeyedBodySQL + `
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = GREATEST(supply_chain_impact_winners_materialization.materialized_at, EXCLUDED.materialized_at)
RETURNING
    (SELECT count(*) FROM changed WHERE is_new),
    (SELECT count(*) FROM changed WHERE NOT is_new),
    (SELECT count(*) FROM deleted),
    (SELECT count(*) FROM winners_now) - (SELECT count(*) FROM changed),
    (SELECT count(*) FROM winners_now) + (SELECT count(*) FROM deleted),
    (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1)
`

var applyWinnersKeyedBodySQL = `
WITH winners_now AS (` + winnersKeyedSelectSQL + `),
changed AS MATERIALIZED (
    SELECT n.*, (w.canonical_key IS NULL) AS is_new
    FROM winners_now AS n
    LEFT JOIN supply_chain_impact_canonical_winners AS w
      ON w.canonical_key = n.canonical_key
    WHERE w.canonical_key IS NULL
       OR ` + supplyChainImpactWinnerRowExpr("n") + ` IS DISTINCT FROM ` + supplyChainImpactWinnerRowExpr("w") + `
),
upserted AS (
    INSERT INTO supply_chain_impact_canonical_winners (
        canonical_key, ` + strings.Join(supplyChainImpactWinnerDataColumns, ", ") + `, materialized_at
    )
    SELECT
        canonical_key, ` + strings.Join(supplyChainImpactWinnerDataColumns, ", ") + `, $1
    FROM changed
    ON CONFLICT (canonical_key) DO UPDATE SET
        ` + supplyChainImpactWinnerConflictSet() + `
),
deleted AS (
    DELETE FROM supply_chain_impact_canonical_winners AS w
    WHERE ` + winnersStoredIdentMatch + `
      AND NOT EXISTS (SELECT 1 FROM winners_now AS n WHERE n.canonical_key = w.canonical_key)
    RETURNING 1
)
`

// stampWinnersWatermarkSQL stamps the watermark candidate for a tick that
// consumed nothing: $1 is the DB-clock tick stamp S when the log is empty, or
// the horizon H = min(captured_at) minus 1us when rows landed after admission
// (#10706). A watermark that only moves when something changed would make an
// idle table read as stale. The stamp is GREATEST(previous, candidate), so the
// watermark never regresses; its RETURNING reads the post-upsert value, so the
// caller pre-reads the replaced value (#11515).
// #nosec G101 -- SQL statement text, not a credential.
const stampWinnersWatermarkSQL = `
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = GREATEST(supply_chain_impact_winners_materialization.materialized_at, EXCLUDED.materialized_at)
RETURNING (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1)
`
