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
//
// n.environment/n.evidence_class are cleared with SET ... = null rather than
// a trailing REMOVE clause: on the pinned NornicDB v1.1.11, a bare REMOVE
// dispatched through the Bolt driver's managed ExecuteWrite transaction (the
// real production reducer path -- cmd/reducer wires this writer's executor
// through reducerNeo4jExecutor.ExecuteGroup, one managed Bolt transaction,
// unconditionally for every backend) fails with
// Neo.ClientError.Statement.SyntaxError ("REMOVE requires a MATCH clause
// first"), even though the identical statement succeeds via plain autocommit
// Execute -- confirmed with a minimal Bolt-driver probe and a live
// bound->unbound regression
// (TestReducerKubernetesNamespaceEnvironmentRetractGraphTruth,
// go/internal/replay/offlinetier). SET prop = null is the openCypher-standard
// property-delete form and, unlike REMOVE, was proven live to apply
// correctly under the same managed-transaction path -- consistent with every
// OTHER property clear in this writer already using SET, never REMOVE. See
// evidence-5434-namespace-environment-retract.md.
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
    n.evidence_source = row.evidence_source,
    n.environment = null,
    n.evidence_class = null`

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

// retractKubernetesNamespaceStaleTargetsEnvironmentCypher removes a
// namespace's PRIOR TARGETS_ENVIRONMENT edge before this generation's write,
// for every row in the batch (bound and unbound alike). The reducer OWNS
// this edge (the same TARGETS_ENVIRONMENT type
// batchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher uses for the
// repo-manifest alias path), so neither
// canonicalKubernetesNamespaceUpsertCypher (which only REMOVEs node
// properties) nor canonicalKubernetesNamespaceWithEnvironmentUpsertCypher
// (which only MERGEs an edge to the row's OWN row.environment, never touches
// an edge to a DIFFERENT prior Environment) ever retracted a stale edge --
// codex review finding P1, #5434. Without this: (a) a namespace that loses
// its recognized environment label keeps asserting the old environment
// forever, and (b) a namespace re-bound from one environment to another
// (e.g. prod -> stage) accumulates a second edge instead of replacing the
// first, since MERGE only matches an edge to the SAME target node.
//
// old_env.name <> row.environment covers both transitions with one
// statement: for an unbound row (row.environment == "") a real Environment
// node's name is never the empty string, so the predicate is unconditionally
// true and any existing edge is deleted; for a bound row it is true only
// when the namespace's environment actually changed, so the steady-state
// case (unchanged binding) matches nothing and never deletes+recreates its
// own edge. Scoped by uid (this writer's own MERGE identity, matching the
// KubernetesNamespace.uid index the upsert MERGE already anchors on) and
// evidence_source (this writer's own edges only), so it never touches an
// edge a different producer wrote to the same Environment node.
const retractKubernetesNamespaceStaleTargetsEnvironmentCypher = `UNWIND $rows AS row
MATCH (n:KubernetesNamespace {uid: row.uid})-[rel:TARGETS_ENVIRONMENT]->(old_env:Environment)
WHERE rel.evidence_source = $evidence_source
  AND old_env.name <> row.environment
DELETE rel`

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
// so it can never create an Environment node. Before either upsert variant
// runs, a retract pass (retractKubernetesNamespaceStaleTargetsEnvironmentCypher,
// dispatched by dispatchRetract) deletes any TARGETS_ENVIRONMENT edge left
// over from a prior generation that no longer matches this row's environment,
// so an unbound-again or re-bound namespace never keeps asserting a stale
// environment (#5434 codex review finding P1). dispatchRetract runs the
// retract with sequential Execute calls, NEVER through GroupExecutor: on the
// pinned NornicDB v1.1.11 a relationship DELETE dispatched through
// ExecuteGroup (the real production reducer executor -- cmd/reducer wires
// KubernetesNamespaceNodeWriter through reducerNeo4jExecutor.ExecuteGroup,
// one managed Bolt transaction, for every backend including NornicDB) can
// under-apply even as the sole statement in the group, while the identical
// statement run auto-commit (Execute) deletes correctly -- the same class of
// defect the cloud-correlation writers fixed via their own dispatchRetract
// helper (see AzureCloudResourceEdgeWriter.dispatchRetract and
// evidence-4367-cloud-edge-retract.md); this writer mirrors that established
// pattern rather than the unrelated NornicDB phase-group Drain mechanism the
// offline canonical projector uses, which this reducer-owned writer's
// executor never routes through. Running the retract to completion before
// building the upsert statements keeps the ordering correct regardless of
// how the upsert batches are dispatched below. The write is idempotent: the
// same uid converges on one node and at most one TARGETS_ENVIRONMENT edge
// across batches, retries, and generations.
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

	all := make([]map[string]any, 0, len(rows))
	var unbound, bound []map[string]any
	for _, row := range rows {
		cloned := make(map[string]any, len(row)+1)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["evidence_source"] = evidenceSource
		all = append(all, cloned)
		if env, _ := cloned["environment"].(string); env != "" {
			bound = append(bound, cloned)
		} else {
			unbound = append(unbound, cloned)
		}
	}

	if err := w.dispatchRetract(ctx, w.buildRetractStatements(all, evidenceSource)); err != nil {
		return err
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

// dispatchRetract runs retract statements sequentially through Execute,
// never through GroupExecutor/ExecuteGroup -- see the dispatch-mechanism note
// on WriteKubernetesNamespaceNodes. Mirrors
// AzureCloudResourceEdgeWriter.dispatchRetract.
func (w *KubernetesNamespaceNodeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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

// buildRetractStatements builds the batched
// retractKubernetesNamespaceStaleTargetsEnvironmentCypher statements for
// every row in this write (bound and unbound alike), for dispatchRetract to
// run sequentially ahead of the upsert statements.
func (w *KubernetesNamespaceNodeWriter) buildRetractStatements(rows []map[string]any, evidenceSource string) []Statement {
	if len(rows) == 0 {
		return nil
	}
	stmts := buildBatchedRetractStatements(retractKubernetesNamespaceStaleTargetsEnvironmentCypher, rows, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters["evidence_source"] = evidenceSource
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseKubernetesNamespace
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = "KubernetesNamespace"
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=TARGETS_ENVIRONMENT retract_stale label=KubernetesNamespace rows=%d",
			len(batchRows),
		)
	}
	return stmts
}
