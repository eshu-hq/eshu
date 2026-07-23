// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// PostureExistenceReader runs a read-only Cypher query and returns row maps.
// The posture node writers that only ever update an already-materialized
// CloudResource node (EC2 internet exposure, EC2 block-device KMS posture,
// RDS posture, S3 internet exposure) use it to confirm which candidate
// CloudResource uids already exist before writing.
//
// Issue #5652: on the pinned production NornicDB image
// (nornicdb-cpu-bge:v1.1.11), an UNWIND-batched statement whose anchor clause
// is a bare `MATCH (n:Label {uid: row.uid})` silently drops its `SET` — the
// statement reports success (PropertiesSet=0, ContainsUpdates=false is the
// ONLY observable signal; matched rows do not distinguish "matched, set
// applied" from "matched, set silently dropped") but the property is never
// written. The identical statement anchored with `MERGE` persists correctly.
// A single-property UNIQUE constraint on the anchored property does NOT fix
// the no-op (measured; see docs/internal/evidence/5652-nornic-bare-match-writeloss.md).
//
// MERGE is not a safe blind substitute here: every one of these four writers
// has an explicit never-create contract (a candidate uid whose CloudResource
// node does not exist must stay absent, never fabricated), and MERGE
// unconditionally creates on a miss. filterRowsToExistingCloudResourceUIDs
// closes that gap: it reads which candidate uids already exist through a
// SEPARATE query first, drops rows for uids that do not, and only the
// confirmed-existing subset ever reaches a MERGE-anchored write — so MERGE
// always matches and never creates.
type PostureExistenceReader interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// postureCloudResourceExistingUIDsCypher is a single-clause UNWIND read: an
// UNWIND-bound candidate value anchors a concrete MATCH and the RETURN alias
// uses a distinct name from the UNWIND binding variable, per the NornicDB
// variable-shadowing pitfall (docs/public/reference/nornicdb-pitfalls.md).
// Reads are not subject to the bare-MATCH SET no-op; this query is safe as
// written and does not need a MERGE workaround.
//
// No RETURN DISTINCT: CloudResource.uid carries a uniqueness constraint plus
// a lookup index (cloud_resource_uid_unique / nornicdb_cloud_resource_uid_lookup,
// see internal/graph.SchemaStatementsForBackend), so a given candidate_uid
// matches at most one CloudResource node — DISTINCT would only guard against a
// duplicate value inside $candidate_uids, which is a hash-aggregation cost
// with no correctness payoff: filterRowsToExistingCloudResourceUIDs below
// folds every returned row into a Go set (existing map[string]struct{})
// before checking membership, so a duplicate row from a duplicate candidate
// uid is deduplicated in Go regardless of what the query returns.
const postureCloudResourceExistingUIDsCypher = `UNWIND $candidate_uids AS candidate_uid
MATCH (resource:CloudResource {uid: candidate_uid})
RETURN resource.uid AS existing_uid`

// filterRowsToExistingCloudResourceUIDs partitions rows (each of which MUST
// carry a non-empty string "uid" key) into the subset whose CloudResource
// node is confirmed, via a separate read, to already exist. A row whose uid
// is missing, empty, or not found by the read is dropped silently — this
// mirrors the never-create contract the bare-MATCH statement already had (a
// missing uid was always a no-op), it is just now enforced in Go instead of
// relying on the anchor clause.
func filterRowsToExistingCloudResourceUIDs(
	ctx context.Context,
	reader PostureExistenceReader,
	rows []map[string]any,
) ([]map[string]any, error) {
	if reader == nil {
		return nil, fmt.Errorf("posture node writer existence reader is required")
	}
	if len(rows) == 0 {
		return nil, nil
	}

	candidateUIDs := make([]any, 0, len(rows))
	for _, row := range rows {
		if uid, ok := row["uid"].(string); ok && uid != "" {
			candidateUIDs = append(candidateUIDs, uid)
		}
	}
	if len(candidateUIDs) == 0 {
		return nil, nil
	}

	records, err := reader.Run(ctx, postureCloudResourceExistingUIDsCypher, map[string]any{
		"candidate_uids": candidateUIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("read existing CloudResource uids: %w", err)
	}

	existing := make(map[string]struct{}, len(records))
	for _, record := range records {
		if uid, ok := record["existing_uid"].(string); ok && uid != "" {
			existing[uid] = struct{}{}
		}
	}

	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		uid, ok := row["uid"].(string)
		if !ok || uid == "" {
			continue
		}
		if _, found := existing[uid]; found {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}
