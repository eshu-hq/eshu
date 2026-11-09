// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"time"
)

// #nosec G101 -- SQL upsert text only; fencing_token is a column name and all values are bound $N parameters, no credential material.
// supplyChainImpactScopePassStateQuery maintains the durable per-scope pass
// state (migration 284) behind the #10389 status degraded reason. One row
// per scope: a partial pass marks it stuck with its identity-affecting
// causes and stamps partial_since when the scope was not already stuck,
// preserving the original stuck time across further partial passes; a full
// complete pass clears the stuck columns, while a scoped complete leaves
// the state untouched because it re-derives only its stamped CVE subset
// (#9779). The fencing-token guard is the admission's <= rule, ordering
// passes by token. Tokens are per-pass monotonic, not per-generation, so the
// token guard alone cannot fence across generations: a stale-generation pass
// admitted later draws a higher token. The generation fence (#10422) skips
// the write, on both the insert and the update path, when a lifecycle-newer
// (ingested_at, generation_id) retained generation is reader-visible
// (activated and active or superseded, the suppression baseline's predicate).
// The fence decides once per statement (materialized CTE), so the decision
// and both write paths see the same committed activations under the writer's
// READ COMMITTED transaction. The statement returns the fence decision
// alongside the write outcome: callers signal on the decision, never on the
// affected-row count, because the token guard can also leave the row
// untouched for a pass that is not stale. A write whose own generation row
// is absent fails open to the token guard, preserving the pre-fence behavior
// where lineage is missing. Through the writer the record step still
// executes fence-open for such a write; the later findings insert then fails
// its scope_generations foreign key and the transaction rolls the record
// back, so the row stays untouched and the write returns the error.
// $4 is the TEXT[] cause set, empty for a complete pass, encoded the same
// way as the retraction's keep set; it never reaches a metric label, so the
// closed cause set needs no folding here. The $5 cast is load-bearing: CASE
// branches over unknown-type params resolve to text, which timestamptz
// rejects (42804); direct $5 uses elsewhere coerce to their columns. $6 is
// the write's generation id, used only by the fence.
const supplyChainImpactScopePassStateQuery = `
WITH stale AS MATERIALIZED (
    SELECT EXISTS (
        SELECT 1 FROM scope_generations AS newer
        WHERE newer.scope_id = $1
          AND newer.generation_id <> $6
          AND newer.activated_at IS NOT NULL
          AND newer.status IN ('active', 'superseded')
          AND (newer.ingested_at, newer.generation_id) > (
              SELECT w.ingested_at, w.generation_id FROM scope_generations AS w
              WHERE w.scope_id = $1 AND w.generation_id = $6)
    ) AS fenced
),
up AS (
    INSERT INTO supply_chain_impact_scope_pass_state (
        scope_id, fencing_token, partial_evidence, partial_causes, partial_since, last_pass_at, updated_at
    )
    SELECT $1, $2, $3, $4,
        CASE WHEN $3 THEN $5::timestamptz ELSE NULL END,
        $5, $5
    WHERE (SELECT NOT fenced FROM stale)
    ON CONFLICT (scope_id) DO UPDATE SET
        fencing_token    = EXCLUDED.fencing_token,
        partial_evidence = EXCLUDED.partial_evidence,
        partial_causes   = EXCLUDED.partial_causes,
        partial_since    = CASE WHEN EXCLUDED.partial_evidence
                                THEN COALESCE(supply_chain_impact_scope_pass_state.partial_since, EXCLUDED.partial_since)
                                ELSE NULL END,
        last_pass_at     = EXCLUDED.last_pass_at,
        updated_at       = EXCLUDED.updated_at
    WHERE supply_chain_impact_scope_pass_state.fencing_token <= EXCLUDED.fencing_token
      AND (SELECT NOT fenced FROM stale)
    RETURNING 1
)
SELECT (SELECT fenced FROM stale) AS fenced, (SELECT count(*) FROM up) AS recorded
`

// recordSupplyChainImpactScopePassState upserts one admitted pass's durable
// per-scope pass state. It runs inside the write transaction, after the
// fencing admission, so the state and the rows it describes commit or roll
// back together. It reports whether the generation fence skipped the write:
// a pass whose generation is stale against a lifecycle-newer reader-visible
// generation (#10422) records nothing and reports true, so the caller can
// signal the skip. The scoped-complete bypass (#9779) is not a skip: it
// reports false without recording.
func recordSupplyChainImpactScopePassState(
	ctx context.Context,
	tx SupplyChainImpactTx,
	write SupplyChainImpactWrite,
	now time.Time,
) (bool, error) {
	// A scoped complete pass re-derives only its stamped CVE subset (#9779),
	// so it must not clear full-scope stuck state it never recomputed: only
	// a full complete pass lifts the degraded reason, and a scoped complete
	// leaves the state untouched. Scoped partial passes still record below:
	// truncation means the scope is partial whatever the scope mode.
	if !write.PartialEvidence && len(write.ScopedCVEIDs) > 0 {
		return false, nil
	}
	// Never record a NULL cause set: the column is NOT NULL and a nil slice
	// would encode as NULL, failing every complete pass. An empty set
	// encodes as '{}', the same shape the retraction's empty keep set uses.
	causes := write.PartialCauses
	if causes == nil {
		causes = []string{}
	}
	rows, err := tx.QueryContext(
		ctx, supplyChainImpactScopePassStateQuery,
		write.ScopeID, write.FencingToken, write.PartialEvidence, causes, now.UTC(), write.GenerationID,
	)
	if err != nil {
		return false, fmt.Errorf("record supply chain impact scope pass state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, fmt.Errorf("record supply chain impact scope pass state: no fence decision row")
	}
	var fenced bool
	var recorded int64
	if err := rows.Scan(&fenced, &recorded); err != nil {
		return false, fmt.Errorf("record supply chain impact scope pass state decision: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("record supply chain impact scope pass state rows: %w", err)
	}
	return fenced, nil
}
