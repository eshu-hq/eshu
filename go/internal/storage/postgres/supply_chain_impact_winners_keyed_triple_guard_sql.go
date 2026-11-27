// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The triple-guarded keyed probe (#9975 round 6): the plain-column twin of
// the shipped payload-expression narrow probe in
// supply_chain_impact_winners_keyed_sql.go, selected per tick by
// winnersKeyedProbeVariant once the key-presence fill marker completes. The
// audit keeps the legacy statement as its oracle.

// winnersKeyedNarrowProbeSelectKeyPresence renders the triple-guarded
// identifier probe body over an identifier source table (#9975 round 6): one
// row per pre-filter copy. It reads the plain supports columns instead of the
// payload: the triple legs carry key-presence guards
// (ident.k <> ” OR (s.key_presence & BIT) <> 0, bits 1/2/4), while the c0
// and advisory legs are plain COALESCE equalities with no guards, preserving
// the shipped leniency for missing, JSON-null, and present-empty advisory
// keys. The MATERIALIZED marker and OFFSET 0 guard are load-bearing for the
// same reason as the shipped probe's. The key-presence fill backfills the
// bits for older rows under its own marker; the tick runs this text only
// after that marker completes (see winnersKeyedProbeVariant), and the audit
// keeps the legacy statement as its oracle.
func winnersKeyedNarrowProbeSelectKeyPresence(identsTable string) string {
	return `WITH narrow AS MATERIALIZED (
    SELECT fact.fact_id, fact.scope_id, fact.generation_id
    FROM ` + identsTable + ` AS ident
    CROSS JOIN LATERAL (
        SELECT s.fact_id, s.scope_id, s.generation_id
        FROM supply_chain_impact_finding_supports AS s
        WHERE s.package_id = ident.package_id
          AND (ident.package_id <> '' OR (s.key_presence & 1) <> 0)
          AND s.repository_id = ident.repository_id
          AND (ident.repository_id <> '' OR (s.key_presence & 2) <> 0)
          AND s.subject_digest = ident.subject_digest
          AND (ident.subject_digest <> '' OR (s.key_presence & 4) <> 0)
          AND COALESCE(NULLIF(s.cve_id, ''), NULLIF(s.advisory_id, ''), '') = ident.c0
          AND s.advisory_id = ident.advisory_id
        OFFSET 0
    ) AS fact
)
SELECT ` + winnersKeyedNarrowColumns + ` FROM narrow
`
}

// buildWinnersKeyedNarrowKeyPresenceSQL persists the triple-guarded probe
// over the whole identifier set: one row per pre-filter copy. It is the
// whole-chunk twin of the budgeted per-identifier insert below (same body,
// staged source table there), so the two cannot silently split; the winners
// contract pins this text. The shipped payload-expression build stays the
// audit oracle and the pre-marker fallback.
var buildWinnersKeyedNarrowKeyPresenceSQL = `
CREATE TEMP TABLE winners_keyed_narrow ON COMMIT DROP AS
` + winnersKeyedNarrowProbeSelectKeyPresence(winnersKeyedIdentsTable)

// insertWinnersKeyedNarrowKeyPresenceBatchSQL probes exactly the staged
// identifier with the triple-guarded probe and appends its copies to the
// narrow table. Same RowsAffected budget contract as the shipped batch
// insert above; the two share nothing but the output columns, so the flip
// between them is explicit (see winnersKeyedProbeVariant), never a silent
// derivation.
var insertWinnersKeyedNarrowKeyPresenceBatchSQL = `INSERT INTO ` + winnersKeyedNarrowTable + `
` + winnersKeyedNarrowProbeSelectKeyPresence(winnersKeyedBatchIdentsTable)

// selectWinnersKeyPresenceFillReadySQL reports whether the key-presence fill
// completed its disagreement-0 verification for the marker ($1). No row, or
// a NULL completed_at, reads as not ready: the tick then runs the shipped
// payload-expression probe, which is correct on unfilled rows, instead of
// the triple-guarded one.
const selectWinnersKeyPresenceFillReadySQL = `
SELECT completed_at IS NOT NULL FROM supply_chain_impact_finding_supports_backfill WHERE marker_name = $1`

// winnersKeyedProbeVariant selects the budgeted narrow probe's per-identifier
// insert for one tick: the triple-guarded text once the key-presence fill
// marker is complete, the shipped payload-expression text before that. The
// default (no marker row) is fail-closed on the shipped text: an unfilled
// row keeps DEFAULT 0 bits, which the guarded probe would exclude from ”
// identifiers, while the shipped probe matches it exactly as before.
func winnersKeyedProbeVariant(keyPresenceReady bool) string {
	if keyPresenceReady {
		return insertWinnersKeyedNarrowKeyPresenceBatchSQL
	}
	return insertWinnersKeyedNarrowBatchSQL
}

// winnersKeyPresenceFillCompleted reports whether the key-presence fill's
// marker completed its disagreement-0 verification. Any read failure,
// including a missing marker row, reads as not ready: the tick then runs the
// shipped probe, so an unreadable gate can never flip the tick onto the
// guarded text.
func winnersKeyPresenceFillCompleted(ctx context.Context, q db.Queryer) bool {
	rows, err := q.QueryContext(ctx, selectWinnersKeyPresenceFillReadySQL,
		SupplyChainImpactKeyPresenceFillMarker)
	if err != nil {
		return false
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false
	}
	var ready bool
	if err := rows.Scan(&ready); err != nil {
		return false
	}
	return ready && rows.Err() == nil
}
