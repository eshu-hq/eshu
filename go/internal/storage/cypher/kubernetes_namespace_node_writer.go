// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseKubernetesNamespace names the live KubernetesNamespace node
// materialization phase for grouped-backend statement metadata and
// diagnostics.
const canonicalPhaseKubernetesNamespace = "kubernetes_namespace"

// canonicalKubernetesNamespaceUpsertCypher batches KubernetesNamespace node
// upserts for a namespace with NO alias-recognized environment label (issue
// #5434). MERGE is on the stable uid identity only (the collector-emitted
// object_id, itself keyed by cluster_id + namespace name); mutable
// properties are SET separately so duplicate input rows and reducer retries
// converge on one node rather than fabricating or duplicating graph state.
// This variant writes NO Environment node and NO TARGETS_ENVIRONMENT edge --
// an unbound namespace must never invent environment truth.
const canonicalKubernetesNamespaceUpsertCypher = `UNWIND $rows AS row
MERGE (n:KubernetesNamespace {uid: row.uid})
SET n.id = row.uid,
    n.cluster_id = row.cluster_id,
    n.namespace = row.namespace,
    n.labels = row.labels,
    n.correlation_anchors = row.correlation_anchors,
    n.environment_state = row.environment_state,
    n.source_fact_id = row.source_fact_id,
    n.stable_fact_key = row.stable_fact_key,
    n.source_system = row.source_system,
    n.source_record_id = row.source_record_id,
    n.source_confidence = row.source_confidence,
    n.collector_kind = row.collector_kind,
    n.evidence_source = row.evidence_source
REMOVE n.environment, n.evidence_class`

// canonicalKubernetesNamespaceWithEnvironmentUpsertCypher is the sibling
// variant for a namespace whose label declared a recognized environment. It
// extends the base upsert with the environment/evidence_class properties and
// a MERGE (:Environment)-bound TARGETS_ENVIRONMENT edge -- the same edge
// type batchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher uses
// for the repo-manifest environment-alias path, so both producers converge
// on one canonical Environment node per name. Only rows the reducer
// classified as environment-bound (a non-empty row.environment) are ever
// routed through this query; see
// KubernetesNamespaceNodeWriter.WriteKubernetesNamespaceNodes.
const canonicalKubernetesNamespaceWithEnvironmentUpsertCypher = `UNWIND $rows AS row
MERGE (n:KubernetesNamespace {uid: row.uid})
SET n.id = row.uid,
    n.cluster_id = row.cluster_id,
    n.namespace = row.namespace,
    n.labels = row.labels,
    n.correlation_anchors = row.correlation_anchors,
    n.environment = row.environment,
    n.environment_state = row.environment_state,
    n.evidence_class = row.evidence_class,
    n.source_fact_id = row.source_fact_id,
    n.stable_fact_key = row.stable_fact_key,
    n.source_system = row.source_system,
    n.source_record_id = row.source_record_id,
    n.source_confidence = row.source_confidence,
    n.collector_kind = row.collector_kind,
    n.evidence_source = row.evidence_source
MERGE (env:Environment {name: row.environment})
MERGE (n)-[env_rel:TARGETS_ENVIRONMENT]->(env)
SET env_rel.evidence_source = row.evidence_source,
    env_rel.evidence_class = row.evidence_class`

// KubernetesNamespaceNodeWriter materializes kubernetes_live.namespace facts
// into canonical KubernetesNamespace graph nodes, binding an Environment node
// only for rows carrying a non-empty "environment" property. It satisfies
// the reducer-owned KubernetesNamespaceNodeWriter consumer interface and
// writes through the backend-neutral Executor seam.
type KubernetesNamespaceNodeWriter struct {
	executor  Executor
	batchSize int
}

// NewKubernetesNamespaceNodeWriter returns a KubernetesNamespaceNodeWriter
// backed by the given Executor. A batchSize of 0 or less uses
// DefaultBatchSize (500).
func NewKubernetesNamespaceNodeWriter(executor Executor, batchSize int) *KubernetesNamespaceNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &KubernetesNamespaceNodeWriter{executor: executor, batchSize: batchSize}
}

// WriteKubernetesNamespaceNodes upserts KubernetesNamespace nodes for the
// given rows using batched UNWIND statements, routing each row to the
// no-environment or with-environment Cypher variant by whether its
// "environment" property is non-empty -- an unbound namespace (row.environment
// == "") NEVER reaches canonicalKubernetesNamespaceWithEnvironmentUpsertCypher,
// so it can never create an Environment node. When the executor implements
// GroupExecutor all batches (across both variants) are dispatched in a single
// atomic transaction; otherwise they run sequentially. The write is
// idempotent: the same uid converges on one node across batches, retries, and
// generations.
func (w *KubernetesNamespaceNodeWriter) WriteKubernetesNamespaceNodes(
	ctx context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("kubernetes namespace node writer executor is required")
	}

	var unbound, bound []map[string]any
	for _, row := range rows {
		cloned := make(map[string]any, len(row)+1)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["evidence_source"] = evidenceSource
		if env, _ := cloned["environment"].(string); env != "" {
			bound = append(bound, cloned)
		} else {
			unbound = append(unbound, cloned)
		}
	}

	stmts := w.buildStatements(unbound, canonicalKubernetesNamespaceUpsertCypher)
	stmts = append(stmts, w.buildStatements(bound, canonicalKubernetesNamespaceWithEnvironmentUpsertCypher)...)
	if len(stmts) == 0 {
		return nil
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

func (w *KubernetesNamespaceNodeWriter) buildStatements(rows []map[string]any, cypherText string) []Statement {
	if len(rows) == 0 {
		return nil
	}
	stmts := buildBatchedStatements(cypherText, rows, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Operation = OperationCanonicalUpsert
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseKubernetesNamespace
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = "KubernetesNamespace"
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=KubernetesNamespace rows=%d",
			len(batchRows),
		)
	}
	return stmts
}
