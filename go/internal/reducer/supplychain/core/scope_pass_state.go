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
// passes by token; it does not fence across generations, so a
// stale-generation pass admitted later draws a higher token and can still
// overwrite current state (#10422).
// $4 is the TEXT[] cause set, empty for a complete pass, encoded the same
// way as the retraction's keep set; it never reaches a metric label, so the
// closed cause set needs no folding here. The $5 cast is load-bearing: CASE
// branches over unknown-type params resolve to text, which timestamptz
// rejects (42804); direct $5 uses elsewhere coerce to their columns.
const supplyChainImpactScopePassStateQuery = `
INSERT INTO supply_chain_impact_scope_pass_state (
    scope_id, fencing_token, partial_evidence, partial_causes, partial_since, last_pass_at, updated_at
) VALUES (
    $1, $2, $3, $4,
    CASE WHEN $3 THEN $5::timestamptz ELSE NULL END,
    $5, $5
)
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
`

// recordSupplyChainImpactScopePassState upserts one admitted pass's durable
// per-scope pass state. It runs inside the write transaction, after the
// fencing admission, so the state and the rows it describes commit or roll
// back together.
func recordSupplyChainImpactScopePassState(
	ctx context.Context,
	tx SupplyChainImpactTx,
	write SupplyChainImpactWrite,
	now time.Time,
) error {
	// A scoped complete pass re-derives only its stamped CVE subset (#9779),
	// so it must not clear full-scope stuck state it never recomputed: only
	// a full complete pass lifts the degraded reason, and a scoped complete
	// leaves the state untouched. Scoped partial passes still record below:
	// truncation means the scope is partial whatever the scope mode.
	if !write.PartialEvidence && len(write.ScopedCVEIDs) > 0 {
		return nil
	}
	// Never record a NULL cause set: the column is NOT NULL and a nil slice
	// would encode as NULL, failing every complete pass. An empty set
	// encodes as '{}', the same shape the retraction's empty keep set uses.
	causes := write.PartialCauses
	if causes == nil {
		causes = []string{}
	}
	if _, err := tx.ExecContext(
		ctx, supplyChainImpactScopePassStateQuery,
		write.ScopeID, write.FencingToken, write.PartialEvidence, causes, now.UTC(),
	); err != nil {
		return fmt.Errorf("record supply chain impact scope pass state: %w", err)
	}
	return nil
}
