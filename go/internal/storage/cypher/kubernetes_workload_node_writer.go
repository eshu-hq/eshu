// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseKubernetesWorkload names the live KubernetesWorkload node
// materialization phase for grouped-backend statement metadata and diagnostics.
const canonicalPhaseKubernetesWorkload = "kubernetes_workload"

// canonicalKubernetesWorkloadUpsertCypher batches KubernetesWorkload node
// upserts. MERGE is on the stable uid identity only (the collector-emitted
// object_id); mutable properties are SET separately so duplicate input rows and
// reducer retries converge on one node rather than fabricating or duplicating
// graph state. The shape mirrors the proven CloudResource canonical writer so it
// engages the same NornicDB schema-backed uid lookup and Neo4j planner path.
//
// The node's uid is the live object identity (cluster_id, api_group, version,
// resource, namespace, name, k8s metadata.uid). workload_uid carries the raw
// Kubernetes metadata.uid as a property only; it is never the node identity,
// because two distinct live objects can share a name across a delete/recreate
// and the object_id already folds the metadata.uid into the identity tuple.
const canonicalKubernetesWorkloadUpsertCypher = `UNWIND $rows AS row
MERGE (w:KubernetesWorkload {uid: row.uid})
SET w.id = row.uid,
    w.cluster_id = row.cluster_id,
    w.namespace = row.namespace,
    w.name = row.name,
    w.workload_uid = row.workload_uid,
    w.group_version_resource = row.group_version_resource,
    w.service_account = row.service_account,
    w.image_refs = row.image_refs,
    w.selector = row.selector,
    w.correlation_anchors = row.correlation_anchors,
    w.source_fact_id = row.source_fact_id,
    w.stable_fact_key = row.stable_fact_key,
    w.source_system = row.source_system,
    w.source_record_id = row.source_record_id,
    w.source_confidence = row.source_confidence,
    w.collector_kind = row.collector_kind,
    w.evidence_source = row.evidence_source`

// KubernetesWorkloadNodeWriter materializes kubernetes_live.pod_template facts
// into canonical KubernetesWorkload graph nodes. It satisfies the reducer-owned
// KubernetesWorkloadNodeWriter consumer interface and writes through the
// backend-neutral Executor seam.
type KubernetesWorkloadNodeWriter struct {
	executor  Executor
	batchSize int
}

// NewKubernetesWorkloadNodeWriter returns a KubernetesWorkloadNodeWriter backed
// by the given Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewKubernetesWorkloadNodeWriter(executor Executor, batchSize int) *KubernetesWorkloadNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &KubernetesWorkloadNodeWriter{executor: executor, batchSize: batchSize}
}

// WriteKubernetesWorkloadNodes upserts KubernetesWorkload nodes for the given
// rows using batched UNWIND statements. When the executor implements
// GroupExecutor all batches are dispatched in a single atomic transaction;
// otherwise they run sequentially. The write is idempotent: the same uid
// converges on one node across batches, retries, and generations.
func (w *KubernetesWorkloadNodeWriter) WriteKubernetesWorkloadNodes(
	ctx context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("kubernetes workload node writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cloned := make(map[string]any, len(row)+1)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["evidence_source"] = evidenceSource
		annotated = append(annotated, cloned)
	}

	stmts := buildBatchedStatements(canonicalKubernetesWorkloadUpsertCypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Operation = OperationCanonicalUpsert
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseKubernetesWorkload
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = "KubernetesWorkload"
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=KubernetesWorkload rows=%d",
			len(batchRows),
		)
	}

	if ge, ok := w.executor.(GroupExecutor); ok {
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		return nil
	}

	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
