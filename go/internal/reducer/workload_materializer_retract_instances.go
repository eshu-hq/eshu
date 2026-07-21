// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
)

// batchWorkloadInstanceRetractCypher DETACH-deletes superseded WorkloadInstance
// nodes by durable id, guarded by a delete-time ownership predicate. DETACH
// DELETE removes every relationship incident to the node — INSTANCE_OF,
// DEPLOYMENT_SOURCE, and RUNS_ON for this label, and any USES edge as
// collateral — so a superseded instance (e.g. a pre-canonical environment
// alias key such as workload-instance:api:production replaced by
// workload-instance:api:prod, #5473) leaves no orphaned edges behind.
//
// The WHERE clause is the CRITICAL 2 fix (round-2 review): instance ids are
// NOT repository-namespaced (workload-instance:<name>:<environment>,
// projection.go) and the MERGE key is id-only, and separate-scope
// WorkloadMaterialization intents can drain concurrently. A retraction
// decision is computed from a Lookup snapshot taken BEFORE this statement
// runs; by the time it runs, a concurrent write for a DIFFERENT repository
// could have re-MERGEd the exact same instance id and overwritten its
// repo_id/evidence_source. Matching by id alone would let that stale decision
// DETACH DELETE a node another scope now owns. Re-validating
// i.repo_id IN $repo_ids AND i.evidence_source = $evidence_source at delete
// time — mirroring retractWorkloadDependencyEdgesCypher in
// storage/cypher/canonical.go — means a row whose current owner no longer
// matches the scope that decided to retract it silently survives instead of
// being deleted: the next materialization pass for its ACTUAL current owner
// will reconcile it correctly.
const batchWorkloadInstanceRetractCypher = `UNWIND $rows AS row
MATCH (i:WorkloadInstance {id: row.instance_id})
WHERE i.repo_id IN $repo_ids AND i.evidence_source = $evidence_source
DETACH DELETE i`

// RetractInstances DETACH-deletes the WorkloadInstance nodes identified by
// instanceIDs, but ONLY those still owned by repoIDs and tagged with
// evidenceSource at delete time (see batchWorkloadInstanceRetractCypher).
// Callers MUST only pass instance ids already confirmed superseded by a
// durable replacement — see ReconcileWorkloadInstanceRetraction, whose
// returned repoIDs MUST be threaded here unmodified so the delete-time
// predicate matches the exact scope the retraction decision was computed
// against — and MUST call this after Materialize has confirmed the
// replacement generation's MERGE write, so a mid-write failure can never
// retract an instance before its canonical replacement is durable.
func (m *WorkloadMaterializer) RetractInstances(
	ctx context.Context,
	instanceIDs []string,
	repoIDs []string,
	evidenceSource string,
) error {
	if len(instanceIDs) == 0 {
		return nil
	}
	if m.executor == nil {
		return fmt.Errorf("workload materializer executor is required")
	}
	if len(repoIDs) == 0 {
		return fmt.Errorf("workload materializer retract instances requires at least one repo id")
	}
	if strings.TrimSpace(evidenceSource) == "" {
		return fmt.Errorf("workload materializer retract instances requires an evidence source")
	}
	rows := make([]map[string]any, len(instanceIDs))
	for i, id := range instanceIDs {
		rows[i] = map[string]any{"instance_id": id}
	}
	return m.executeBatchedWithParams(ctx, batchWorkloadInstanceRetractCypher, rows, map[string]any{
		"repo_ids":        repoIDs,
		"evidence_source": evidenceSource,
	})
}
