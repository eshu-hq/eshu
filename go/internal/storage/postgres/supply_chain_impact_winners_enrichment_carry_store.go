// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// This file implements the durable side of the #11466 enrichment
// first-carry stamp (supply_chain_impact_winners_enrichment_first_carry,
// migration 359): one row per (canonical_key, enrichment_source) holding
// the first instant the winners-visible row carried that source's current
// value, claimed set-once per value inside the resweep transaction.
//
// The carry runs after the rebuild on the full path (and the over-cap full
// fallback, which shares the rebuild) and after the keyed apply on the
// keyed path, in the same transaction. The INSERT reads the just-reconciled
// winners rows, so a value the resweep just wrote is stamped with the same
// stamp S the resweep stamped its row version with, and a concurrent reader
// never sees a winner without its carry. The ON CONFLICT guard updates only
// when the incoming value_identity IS DISTINCT FROM the stored one: a
// rewrite or a same-value re-ingest that resolves the same value moves
// nothing (no WAL churn on unchanged values, no live QA writes during the
// drain), and only a value change (EPSS rescore, KEV flip) advances the
// stamp. A claim failure fails the resweep: a reconciled winner without its
// stamp would silently report an unmeasurable lag.
//
// The value identities are read from the winner payload's existing fields,
// never written back: selected_epss_score_date provenance for first_epss
// (absent or empty selects no row), and the top-level known_exploited flag
// for cisa_kev (missing reads as false). No payload, SDK, or factschema
// change backs this stamp. The closed source pair is enforced by the
// table's CHECK; widening it takes a new migration per #7002.
//
// The carry statements are NOT part of the winners contract pin
// (supplyChainImpactWinnersContractStatements): they never change which
// winners a resweep produces for the same facts, only the stamp table beside
// them, so no contract bump or re-pin rides with them.

// claimSupplyChainImpactWinnersEnrichmentCarryConflictTail is the shared
// set-once-per-value guard: an incoming value that matches the stored one
// writes nothing, so rewrites and same-value re-ingests never move the
// stamp or churn WAL.
const claimSupplyChainImpactWinnersEnrichmentCarryConflictTail = `
ON CONFLICT (canonical_key, enrichment_source) DO UPDATE SET
    value_identity = EXCLUDED.value_identity,
    first_carried_at = EXCLUDED.first_carried_at
WHERE supply_chain_impact_winners_enrichment_first_carry.value_identity IS DISTINCT FROM EXCLUDED.value_identity`

// claimSupplyChainImpactWinnersEnrichmentCarryFullSQL stamps every current
// winner in the resweep transaction. $1 is the resweep stamp S: the same
// instant the rebuild stamped the row versions it (re)wrote, so a value
// carried by a changed row is stamped with its row's own materialized_at.
// A never-stamped value on an unchanged row (for example a value that
// landed before this stamp table deployed) is stamped at S and reads as a
// backfill the cohort rule excludes by T0; it is never back-estimated.
const claimSupplyChainImpactWinnersEnrichmentCarryFullSQL = `
INSERT INTO supply_chain_impact_winners_enrichment_first_carry
    (canonical_key, enrichment_source, value_identity, first_carried_at)
SELECT canonical_key, enrichment_source, value_identity, $1
FROM (
    SELECT w.canonical_key,
        'first_epss' AS enrichment_source,
        NULLIF(w.payload->'provenance'->>'selected_epss_score_date', '') AS value_identity
    FROM supply_chain_impact_canonical_winners AS w
    UNION ALL
    SELECT w.canonical_key,
        'cisa_kev' AS enrichment_source,
        COALESCE(w.payload->>'known_exploited', 'false') AS value_identity
    FROM supply_chain_impact_canonical_winners AS w
) AS carried
WHERE carried.value_identity IS NOT NULL` + claimSupplyChainImpactWinnersEnrichmentCarryConflictTail

// claimSupplyChainImpactWinnersEnrichmentCarryKeyedSQL stamps only the
// identifier set the keyed tick recomputed, through the same stored-to-sets
// match the keyed apply's delete uses. A key outside the set did not change
// on this tick, so its stamp (or its absence, which only a full resweep
// backfills) is already correct; scanning only the set keeps the keyed
// tick's cost proportional to the tick, not the table.
const claimSupplyChainImpactWinnersEnrichmentCarryKeyedSQL = `
INSERT INTO supply_chain_impact_winners_enrichment_first_carry
    (canonical_key, enrichment_source, value_identity, first_carried_at)
SELECT canonical_key, enrichment_source, value_identity, $1
FROM (
    SELECT w.canonical_key,
        'first_epss' AS enrichment_source,
        NULLIF(w.payload->'provenance'->>'selected_epss_score_date', '') AS value_identity
    FROM supply_chain_impact_canonical_winners AS w
    WHERE ` + winnersStoredIdentMatch + `
    UNION ALL
    SELECT w.canonical_key,
        'cisa_kev' AS enrichment_source,
        COALESCE(w.payload->>'known_exploited', 'false') AS value_identity
    FROM supply_chain_impact_canonical_winners AS w
    WHERE ` + winnersStoredIdentMatch + `
) AS carried
WHERE carried.value_identity IS NOT NULL` + claimSupplyChainImpactWinnersEnrichmentCarryConflictTail

// claimSupplyChainImpactWinnersEnrichmentFirstCarry claims the #11466
// carry stamps for the winners the resweep just reconciled. keyed selects
// the identifier-set statement for a keyed tick; every other resweep runs
// the full statement. stamp is the resweep's row-version stamp S (or the
// keyed candidate), so a newly carried value shares its row's
// materialized_at. A claim failure fails the resweep: an unstamped winner
// would silently drop out of the arrival-to-rescore cohort.
func claimSupplyChainImpactWinnersEnrichmentFirstCarry(
	ctx context.Context,
	tx db.Transaction,
	keyed bool,
	stamp time.Time,
) error {
	statement := claimSupplyChainImpactWinnersEnrichmentCarryFullSQL
	if keyed {
		statement = claimSupplyChainImpactWinnersEnrichmentCarryKeyedSQL
	}
	if _, err := tx.ExecContext(ctx, statement, stamp); err != nil {
		return fmt.Errorf("claim supply chain impact winners enrichment first carry: %w", err)
	}
	return nil
}
