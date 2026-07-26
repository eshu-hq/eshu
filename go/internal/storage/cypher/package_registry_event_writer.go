// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// canonicalPhasePackageRegistryEvents is the node-write phase for
// RegistryEvent nodes. canonicalPhasePackageRegistryEventEdges is the
// deferred edge-write phase for the HAS_REGISTRY_EVENT edge from the owning
// PackageVersion, split out for the same NornicDB read-your-writes reason
// documented on package_registry_artifact_writer.go: a node created with
// multiple labels (RegistryEvent:PackageRegistryRegistryEvent) is not visible
// to a same-transaction UNWIND-driven MATCH, so the edge MUST run after the
// node phases commit.
const (
	canonicalPhasePackageRegistryEvents     = "package_registry_events"
	canonicalPhasePackageRegistryEventEdges = "package_registry_event_edges"
)

// canonicalPackageRegistryEventUpsertCypher MERGEs one RegistryEvent node by
// its stable_fact_key-derived uid. This is the #5458 yank/deprecate/publish
// TIMELINE the epic names: before this writer existed,
// package_registry.registry_event facts were collected and read by nothing.
const canonicalPackageRegistryEventUpsertCypher = `UNWIND $rows AS row
MERGE (e:RegistryEvent:PackageRegistryRegistryEvent {uid: row.uid})
SET e.id = row.uid,
    e.package_id = row.package_id,
    e.version_id = row.version_id,
    e.version = row.version,
    e.ecosystem = row.ecosystem,
    e.registry = row.registry,
    e.event_key = row.event_key,
    e.event_type = row.event_type,
    e.artifact_key = row.artifact_key,
    e.actor = row.actor,
    e.message = row.message,
    e.occurred_at = row.occurred_at,
    e.source_fact_id = row.source_fact_id,
    e.stable_fact_key = row.stable_fact_key,
    e.source_system = row.source_system,
    e.source_record_id = row.source_record_id,
    e.source_confidence = row.source_confidence,
    e.collector_kind = row.collector_kind,
    e.collector_instance_id = row.collector_instance_id,
    e.correlation_anchors = row.correlation_anchors,
    e.scope_id = row.scope_id,
    e.generation_id = row.generation_id,
    e.evidence_source = 'projector/package_registry'`

// canonicalPackageRegistryEventEdgeCypher is the deferred HAS_REGISTRY_EVENT
// edge MERGE, run in the second write group after the PackageVersion and
// RegistryEvent node phases commit. The PackageVersion MATCH pins package_id:
// row.package_id in addition to uid: row.version_id (mirroring the #5820 P2
// review finding fixed on the HAS_ARTIFACT edge in
// package_registry_artifact_writer.go): a malformed-but-schema-valid
// registry_event fact can carry a package_id that names package A while its
// version_id resolves to a version node genuinely owned by package B.
// Anchoring on uid alone would still MATCH that version and MERGE an edge
// from B's version to an event node that itself claims package_id A --
// internally contradictory package graph truth. Requiring both properties on
// the same node means a mismatched pair finds no row (MATCH drops that
// UNWIND row, no edge written) instead of silently cross-attaching the event
// to the wrong package's version. row.package_id is the event's own claimed
// package_id (packageRegistryEventRows), and v.package_id is the value the
// PackageVersion node itself was written with
// (canonicalPackageRegistryVersionUpsertCypher's v.package_id =
// row.package_id in package_registry_canonical_writer.go), so the predicate
// only passes when the two facts agree on which package the version belongs
// to.
const canonicalPackageRegistryEventEdgeCypher = `UNWIND $rows AS row
MATCH (v:PackageVersion {uid: row.version_id, package_id: row.package_id})
MATCH (e:RegistryEvent {uid: row.uid})
MERGE (v)-[rel:HAS_REGISTRY_EVENT]->(e)
SET rel.generation_id = row.generation_id,
    rel.evidence_source = 'projector/package_registry'`

// buildPackageRegistryEventStatements emits the RegistryEvent node-upsert
// statements for the main write group.
func (w *CanonicalNodeWriter) buildPackageRegistryEventStatements(
	mat projector.CanonicalMaterialization,
) []Statement {
	return packageRegistryBatchedStatements(
		canonicalPackageRegistryEventUpsertCypher,
		packageRegistryEventRows(mat),
		w.batchSize,
		"PackageRegistryRegistryEvent",
		canonicalPhasePackageRegistryEvents,
		mat,
	)
}

// buildPackageRegistryEventEdgeStatements emits the deferred
// HAS_REGISTRY_EVENT edge MERGEs that attach each RegistryEvent to its owning
// PackageVersion. It runs as a separate write group after the PackageVersion
// and RegistryEvent node phases commit, for the same NornicDB
// read-your-writes reason as the version, dependency, and artifact edge
// phases in package_registry_edge_writer.go and
// package_registry_artifact_writer.go.
func (w *CanonicalNodeWriter) buildPackageRegistryEventEdgeStatements(
	mat projector.CanonicalMaterialization,
) []Statement {
	return packageRegistryBatchedStatements(
		canonicalPackageRegistryEventEdgeCypher,
		packageRegistryEventRows(mat),
		w.batchSize,
		"PackageRegistryEventEdge",
		canonicalPhasePackageRegistryEventEdges,
		mat,
	)
}

// packageRegistryEventRows converts the materialization's event rows into the
// parameter-map shape the upsert and edge Cypher both UNWIND. The same row
// shape serves both statements (the edge Cypher only reads uid, version_id,
// and generation_id, mirroring packageRegistryArtifactRows' dual-purpose
// reuse for the HAS_ARTIFACT edge).
func packageRegistryEventRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.PackageRegistryEvents))
	for _, row := range mat.PackageRegistryEvents {
		rows = append(rows, map[string]any{
			"uid":                   row.UID,
			"package_id":            row.PackageID,
			"version_id":            row.VersionID,
			"version":               row.Version,
			"ecosystem":             row.Ecosystem,
			"registry":              row.Registry,
			"event_key":             row.EventKey,
			"event_type":            row.EventType,
			"artifact_key":          row.ArtifactKey,
			"actor":                 row.Actor,
			"message":               row.Message,
			"occurred_at":           packageRegistryTimeValue(row.OccurredAt),
			"source_fact_id":        row.SourceFactID,
			"stable_fact_key":       row.StableFactKey,
			"source_system":         row.SourceSystem,
			"source_record_id":      row.SourceRecordID,
			"source_confidence":     row.SourceConfidence,
			"collector_kind":        row.CollectorKind,
			"collector_instance_id": row.CollectorInstanceID,
			"correlation_anchors":   row.CorrelationAnchors,
			"scope_id":              mat.ScopeID,
			"generation_id":         mat.GenerationID,
		})
	}
	return rows
}
