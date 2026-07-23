// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
)

// crossplaneSatisfiedByEdgeExistsCypher confirms, per (claim_uid, xrd_uid)
// pair, whether a SATISFIED_BY edge actually exists in the graph (issue
// #5476 P1-b). cypher.CrossplaneSatisfiedByEdgeWriter's MATCH-MATCH-MERGE
// deliberately no-ops -- returning nil, not an error -- when either endpoint
// node is absent (self-healing by design: the edge commits on a later
// generation once both endpoints exist), so WriteCrossplaneSatisfiedByEdges
// returning nil does NOT prove every row actually got an edge. This read
// re-confirms the concrete fact after the write.
//
// The UNWIND-bound variable ("candidate") is deliberately distinct from both
// RETURN aliases, per the NornicDB UNWIND variable-shadowing pitfall
// (docs/public/reference/nornicdb-pitfalls.md): reusing the UNWIND binding
// name as a RETURN alias silently returns zero rows with no error on the
// pinned backends. This statement is read-only (RETURN, no SET), so it is
// not subject to the separate bare-MATCH SET no-op (issue #5652) that
// motivated the analogous postureCloudResourceExistingUIDsCypher pattern
// this mirrors.
const crossplaneSatisfiedByEdgeExistsCypher = `UNWIND $rows AS candidate
MATCH (claim:K8sResource {uid: candidate.claim_uid})-[:SATISFIED_BY]->(xrd:CrossplaneXRD {uid: candidate.xrd_uid})
RETURN candidate.claim_uid AS confirmed_claim_uid, candidate.xrd_uid AS confirmed_xrd_uid`

// confirmWrittenCrossplaneSatisfiedByEdges filters rows down to the subset
// whose SATISFIED_BY edge is confirmed present in the graph, post-write
// (issue #5476 P1-b). Recording an unconfirmed row into the redrive ledger
// would permanently fence a target this handler never actually satisfied --
// the exact silent false-negative the ledger exists to prevent -- so a row
// whose edge cannot be confirmed is simply dropped, never fenced: the next
// cross-scope redrive sweep re-enqueues it (correct, if wasteful), matching
// this file's own "never under-inclusion, but also never a false positive"
// ledger doctrine (see recordRedriveLedger's doc comment).
//
// h.EdgeExistenceReader nil returns (nil, nil) -- no rows confirmed, no
// graph read attempted -- so the caller's ledger write is skipped entirely
// for every row rather than assuming success. This is the safe default: an
// unwired reader must never let this handler blindly re-adopt the
// pre-#5476-P1-b behavior of trusting a nil write error as proof of a
// committed edge.
func (h CrossplaneSatisfiedByMaterializationHandler) confirmWrittenCrossplaneSatisfiedByEdges(
	ctx context.Context,
	rows []map[string]any,
) ([]map[string]any, error) {
	if h.EdgeExistenceReader == nil || len(rows) == 0 {
		return nil, nil
	}

	candidates := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, map[string]any{
			"claim_uid": row["claim_uid"],
			"xrd_uid":   row["xrd_uid"],
		})
	}

	confirmed, err := h.EdgeExistenceReader.Run(ctx, crossplaneSatisfiedByEdgeExistsCypher, map[string]any{
		"rows": candidates,
	})
	if err != nil {
		return nil, fmt.Errorf("confirm written crossplane satisfied-by edges: %w", err)
	}

	confirmedPairs := make(map[string]struct{}, len(confirmed))
	for _, record := range confirmed {
		claimUID := anyToString(record["confirmed_claim_uid"])
		xrdUID := anyToString(record["confirmed_xrd_uid"])
		if claimUID == "" || xrdUID == "" {
			continue
		}
		confirmedPairs[claimUID+"->"+xrdUID] = struct{}{}
	}

	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		key := anyToString(row["claim_uid"]) + "->" + anyToString(row["xrd_uid"])
		if _, ok := confirmedPairs[key]; ok {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}
