// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
)

// listStoredSupplyChainImpactFindingsQuery reads the finding rows of the
// generation being written and of the scope's previous generation from the
// per-support table (#10880 PR-C1 b): the rows the pass may rewrite unchanged, the
// rows its retraction may tombstone, and the baseline of a first pass. It replaces
// the read of fact_records so the writer's input no longer needs the finding facts.
//
// Every payload-derived column is the legacy expression over the support's
// payload, which is the fact payload verbatim; cve_id and advisory_id are the
// support's derived columns, the same COALESCE over the same payload keys.
// The identity digest (provenance.identity_digest, D7) rides alongside the
// content digest so the fan-out origin gate judges identity change; a support
// written before it existed reads empty and fails closed, as the fact read does.
// Supports hold only live rows (a retraction deletes the support with the fact's
// tombstone), so the tombstone predicate has no counterpart.
//
// The stable key is the support's own, falling back to the fact's key through a
// primary-key scalar subquery for a support written before the column existed
// (migration 350). The subquery is the second COALESCE argument, which COALESCE
// evaluates only when the support's key is NULL, so a filled key costs no
// fact_records probe (a join would probe once per row either way). A row with
// neither key reads as an empty key, which the reader replaces with a key nothing
// can match. The fencing token is the constant 0: the per-row guard is
// redundant with whole-pass admission (the fact id embeds scope and generation and
// the pair lock and admission reject a stale pass), so a stored row never reads as
// fresher than the pass. The generation window is the shared previous-generation
// subquery. Arguments: $1 scope id, $2 generation id.
const listStoredSupplyChainImpactFindingsQuery = `
SELECT s.fact_id,
       s.generation_id,
       COALESCE(s.stable_fact_key, (SELECT twin.stable_fact_key FROM fact_records AS twin WHERE twin.fact_id = s.fact_id), ''),
       COALESCE(s.payload #>> '{provenance,content_digest}', ''),
       COALESCE(s.payload #>> '{provenance,identity_digest}', ''),
       s.cve_id,
       s.advisory_id,
       0::bigint,
       COALESCE(s.payload #>> '{provenance,advisory_group_members}', '[]'),
       COALESCE(NULLIF(s.payload ->> 'suppression_state', ''), 'active'),
       COALESCE(s.payload #>> '{evidence_fact_ids}', '[]')
FROM supply_chain_impact_finding_supports AS s
WHERE s.scope_id = $1
  AND s.generation_id IN ($2, ` + priorGenerationSupplyChainImpactSQL + `)
`

// missingStableKeyPrefix starts the key a stored row with no stable key is read
// under. The key embeds the fact id, so it is unique and no derived row can carry
// it: the pass treats the stored row as a finding it did not derive again, and the
// derived row it would have matched as new, which is the safe reading (changed).
const missingStableKeyPrefix = "missing-stable-key:"

// SupplyChainImpactStoredKeyRecorder is implemented by a writer transaction that
// counts the stored rows whose stable key was missing on both the support and its
// fact twin, once per read, so an operator sees how many findings the pass had to
// treat as changed for want of a key. A transaction without it reads the same rows
// and records nothing.
type SupplyChainImpactStoredKeyRecorder interface {
	RecordStoredKeyMissing(ctx context.Context, rows int64)
}

// readStoredSupplyChainImpactFindings returns the rows of the generation being
// written and of the scope's previous generation, in the order the database
// returns them.
func readStoredSupplyChainImpactFindings(
	ctx context.Context,
	tx SupplyChainImpactTx,
	scopeID string,
	generationID string,
) ([]storedFinding, error) {
	rows, err := tx.QueryContext(ctx, listStoredSupplyChainImpactFindingsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("read stored supply chain impact findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var stored []storedFinding
	var keyMissing int64
	for rows.Next() {
		var row storedFinding
		var membersRaw, evidenceRaw string
		if err := rows.Scan(&row.factID, &row.generationID, &row.stableKey, &row.digest, &row.identityDigest, &row.cveID, &row.advisoryID, &row.fencingToken, &membersRaw, &row.suppressionState, &evidenceRaw); err != nil {
			return nil, fmt.Errorf("scan stored supply chain impact finding: %w", err)
		}
		if row.stableKey == "" {
			row.stableKey = missingStableKeyPrefix + row.factID
			keyMissing++
		}
		members, err := parseAdvisoryGroupMembers(membersRaw)
		if err != nil {
			return nil, fmt.Errorf("decode stored supply chain impact finding members: %w", err)
		}
		row.members = members
		evidence, err := parseEvidenceFactIDs(evidenceRaw)
		if err != nil {
			return nil, fmt.Errorf("decode stored supply chain impact finding evidence fact ids: %w", err)
		}
		row.evidenceFactIDs = evidence
		stored = append(stored, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stored supply chain impact findings: %w", err)
	}
	if recorder, ok := tx.(SupplyChainImpactStoredKeyRecorder); ok && keyMissing > 0 {
		recorder.RecordStoredKeyMissing(ctx, keyMissing)
	}
	return stored, nil
}
