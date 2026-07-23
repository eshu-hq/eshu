// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// Graph provenance edges project two reducer correlation domains into the
// canonical graph (docs/internal/design/5472-graph-projection-policy.md):
// Repository-[:PUBLISHES]->Package|PackageVersion from package
// ownership/publication correlation, and
// ContainerImage-[:BUILT_FROM]->Repository from container image identity
// correlation.
//
// Package, PackageVersion, and ContainerImage all carry a second label
// (PackageRegistryPackage/PackageRegistryPackageVersion, OciImageManifest).
// NornicDB does not make a node MERGE'd with multiple labels in one atomic
// transaction visible to a later same-transaction UNWIND-driven MATCH
// (package_registry_edge_writer.go), so these edge writes must run in a
// transaction that starts after the node-owning transaction commits. This
// writer dispatches through its own Executor call rather than joining the
// ingestion CanonicalNodeWriter's atomic group, and reducer correlation
// intents (package_source_correlation, container_image_identity) only run
// once their source facts are active in Postgres — so the deferred-write
// ordering the #5472 policy requires holds by construction, without needing a
// second in-process write group of its own.
const (
	canonicalPhaseProvenancePublishesEdges = "provenance_publishes_edges"
	canonicalPhaseProvenanceBuiltFromEdges = "provenance_built_from_edges"
)

// provenancePublishesEdgeLabel and provenanceBuiltFromEdgeLabel tag the
// bounded entity-label statement-metadata dimension for the two edge types
// this writer projects.
const (
	provenancePublishesEdgeLabel = "PUBLISHES"
	provenanceBuiltFromEdgeLabel = "BUILT_FROM"
)

// canonicalProvenancePublishesPackageCypher upserts a PUBLISHES edge from a
// Repository to a Package. Two MATCHes precede the MERGE so a row whose
// repository or package node is absent produces no edge and no fabricated
// node (the missing-endpoint no-op contract, #5472).
const canonicalProvenancePublishesPackageCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repository_id})
MATCH (target:Package {uid: row.package_id})
MERGE (repo)-[rel:PUBLISHES]->(target)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// canonicalProvenancePublishesPackageVersionCypher upserts a PUBLISHES edge
// from a Repository to a PackageVersion, for correlation decisions bound to a
// specific published version rather than the package as a whole.
const canonicalProvenancePublishesPackageVersionCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repository_id})
MATCH (target:PackageVersion {uid: row.version_id})
MERGE (repo)-[rel:PUBLISHES]->(target)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// canonicalProvenanceBuiltFromCypher upserts a BUILT_FROM edge from a
// ContainerImage to the Repository its container_image_identity decision
// resolved as source. The ContainerImage endpoint is matched by digest, not
// uid: ContainerImageIdentityDecision (go/internal/reducer/container_image_identity.go)
// carries only ImageRef/Digest, never the OciImageManifest node uid, and
// oci_registry_canonical_writer.go SETs digest on every ContainerImage node it
// writes, so digest is the correct and only available join key.
const canonicalProvenanceBuiltFromCypher = `UNWIND $rows AS row
MATCH (img:ContainerImage {digest: row.digest})
MATCH (repo:Repository {id: row.repository_id})
MERGE (img)-[rel:BUILT_FROM]->(repo)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractProvenancePublishesEdgesCypher removes this writer's PUBLISHES edges
// for one scope+evidence_source before a fresh generation reprojects them.
const retractProvenancePublishesEdgesCypher = `MATCH (:Repository)-[rel:PUBLISHES]->()
WHERE rel.scope_id = $scope_id
  AND rel.evidence_source = $evidence_source
DELETE rel`

// retractProvenanceBuiltFromEdgesCypher removes this writer's BUILT_FROM
// edges for one scope+evidence_source before a fresh generation reprojects
// them. BUILT_FROM is a shared edge type with the #5428
// reducer/ci-cd-run-correlation domain; the evidence_source predicate is what
// keeps this retract from ever touching that domain's edges.
const retractProvenanceBuiltFromEdgesCypher = `MATCH (:ContainerImage)-[rel:BUILT_FROM]->(:Repository)
WHERE rel.scope_id = $scope_id
  AND rel.evidence_source = $evidence_source
DELETE rel`

// ProvenanceEdgeWriter persists and retracts the PUBLISHES and BUILT_FROM
// graph provenance edges from Postgres reducer correlation decisions.
// Implementations MUST be idempotent by (source id/uid, edge type, target
// id/uid) so retries and re-projected generations converge on one edge, and
// MUST NOT fabricate an endpoint node: a row whose source or target node is
// absent is a no-op, counted skipped by the caller.
type ProvenanceEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewProvenanceEdgeWriter returns a ProvenanceEdgeWriter backed by the given
// Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewProvenanceEdgeWriter(executor Executor, batchSize int) *ProvenanceEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &ProvenanceEdgeWriter{executor: executor, batchSize: batchSize}
}

// WritePublishesEdges upserts PUBLISHES edges for the given rows. Each row
// MUST carry repository_id and exactly one of package_id or version_id; rows
// are bucketed by which target key is present so the Package and
// PackageVersion targets use their own MATCH-typed Cypher (NornicDB does not
// reliably support a node-label disjunction MATCH, the #5116 sibling
// constraint). scopeID, generationID, and evidenceSource are stamped onto
// every edge.
func (w *ProvenanceEdgeWriter) WritePublishesEdges(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("provenance edge writer executor is required")
	}

	var packageRows, versionRows []map[string]any
	for _, row := range rows {
		cloned := cloneProvenanceRow(row, scopeID, generationID, evidenceSource)
		if versionID, ok := cloned["version_id"].(string); ok && versionID != "" {
			versionRows = append(versionRows, cloned)
			continue
		}
		packageRows = append(packageRows, cloned)
	}

	var stmts []Statement
	stmts = append(stmts, tagProvenanceStatements(
		buildBatchedStatements(canonicalProvenancePublishesPackageCypher, packageRows, w.batchSize),
		canonicalPhaseProvenancePublishesEdges, provenancePublishesEdgeLabel, "target=Package",
	)...)
	stmts = append(stmts, tagProvenanceStatements(
		buildBatchedStatements(canonicalProvenancePublishesPackageVersionCypher, versionRows, w.batchSize),
		canonicalPhaseProvenancePublishesEdges, provenancePublishesEdgeLabel, "target=PackageVersion",
	)...)

	return w.dispatch(ctx, stmts)
}

// WriteBuiltFromEdges upserts BUILT_FROM edges for the given rows. Each row
// MUST carry digest and repository_id. scopeID, generationID, and
// evidenceSource are stamped onto every edge.
func (w *ProvenanceEdgeWriter) WriteBuiltFromEdges(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("provenance edge writer executor is required")
	}

	cloned := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cloned = append(cloned, cloneProvenanceRow(row, scopeID, generationID, evidenceSource))
	}

	stmts := tagProvenanceStatements(
		buildBatchedStatements(canonicalProvenanceBuiltFromCypher, cloned, w.batchSize),
		canonicalPhaseProvenanceBuiltFromEdges, provenanceBuiltFromEdgeLabel, "target=Repository",
	)
	return w.dispatch(ctx, stmts)
}

// RetractPublishesEdges removes this writer's PUBLISHES edges for one
// scope+evidence_source. It is a no-op for a blank scopeID. The delete is
// dispatched as a sequential autocommit Execute, never ExecuteGroup: on the
// pinned NornicDB v1.1.11 a DELETE dispatched through a managed transaction
// under-applies, even a single statement (see
// kubernetes_correlation_edge_writer.go dispatchRetract and
// docs/public/reference/nornicdb-pitfalls.md).
func (w *ProvenanceEdgeWriter) RetractPublishesEdges(
	ctx context.Context,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	return w.retract(
		ctx, retractProvenancePublishesEdgesCypher,
		canonicalPhaseProvenancePublishesEdges, provenancePublishesEdgeLabel,
		scopeID, generationID, evidenceSource,
	)
}

// RetractBuiltFromEdges removes this writer's BUILT_FROM edges for one
// scope+evidence_source. See RetractPublishesEdges for the
// sequential-autocommit dispatch rationale.
func (w *ProvenanceEdgeWriter) RetractBuiltFromEdges(
	ctx context.Context,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	return w.retract(
		ctx, retractProvenanceBuiltFromEdgesCypher,
		canonicalPhaseProvenanceBuiltFromEdges, provenanceBuiltFromEdgeLabel,
		scopeID, generationID, evidenceSource,
	)
}

func (w *ProvenanceEdgeWriter) retract(
	ctx context.Context,
	cypher string,
	phase string,
	label string,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if scopeID == "" {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("provenance edge writer executor is required")
	}
	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    cypher,
		Parameters: map[string]any{
			"scope_id":                      scopeID,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       phase,
			StatementMetadataEntityLabelKey: label,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scope=%s generation=%s evidence_source=%s",
				label, scopeID, generationID, evidenceSource,
			),
		},
	}
	return w.dispatchRetract(ctx, []Statement{stmt})
}

// cloneProvenanceRow copies row and stamps the reducer-scoped provenance
// fields the resolution layer does not carry: scope_id (what the
// prior-generation retract filters on), generation_id, and evidence_source.
func cloneProvenanceRow(row map[string]any, scopeID, generationID, evidenceSource string) map[string]any {
	cloned := make(map[string]any, len(row)+3)
	for k, v := range row {
		cloned[k] = v
	}
	cloned["scope_id"] = scopeID
	cloned["generation_id"] = generationID
	cloned["evidence_source"] = evidenceSource
	return cloned
}

func tagProvenanceStatements(stmts []Statement, phase, label, summarySuffix string) []Statement {
	for i := range stmts {
		rows, _ := stmts[i].Parameters["rows"].([]map[string]any)
		stmts[i].Parameters[StatementMetadataPhaseKey] = phase
		stmts[i].Parameters[StatementMetadataEntityLabelKey] = label
		stmts[i].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s %s rows=%d", label, summarySuffix, len(rows),
		)
	}
	return stmts
}

// dispatch runs the prepared upsert statements as one atomic group when the
// executor supports grouping, otherwise sequentially.
func (w *ProvenanceEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

// dispatchRetract routes retract statements through sequential Execute calls,
// never ExecuteGroup. On the pinned NornicDB v1.1.11 a DELETE dispatched
// through ExecuteGroup / a managed transaction under-applies -- even a single
// statement -- while the identical statement run as an auto-commit
// transaction (Execute) deletes correctly. See
// docs/public/reference/nornicdb-pitfalls.md and
// KubernetesCorrelationEdgeWriter.dispatchRetract for the same rationale.
func (w *ProvenanceEdgeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
