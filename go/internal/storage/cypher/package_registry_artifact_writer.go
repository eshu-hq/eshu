// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// canonicalPhasePackageRegistryArtifacts is the node-write phase for
// PackageArtifact nodes. canonicalPhasePackageRegistryArtifactEdges is the
// deferred edge-write phase for the HAS_ARTIFACT edge from the owning
// PackageVersion, split out for the same NornicDB read-your-writes reason
// documented on package_registry_edge_writer.go: a node created with multiple
// labels (PackageArtifact:PackageRegistryPackageArtifact) is not visible to a
// same-transaction UNWIND-driven MATCH, so the edge MUST run after the node
// phases commit.
const (
	canonicalPhasePackageRegistryArtifacts     = "package_registry_artifacts"
	canonicalPhasePackageRegistryArtifactEdges = "package_registry_artifact_edges"
)

// canonicalPackageRegistryArtifactUpsertCypher MERGEs one PackageArtifact node
// by its stable_fact_key-derived uid. hashes is stored as a sorted
// "algorithm:digest" string list rather than a nested map (Cypher node
// properties cannot hold a map), which is the #5458 fix: the owning
// PackageVersion node only ever stored checksum_algorithms (algorithm NAMES),
// dropping the actual per-artifact digest. Each entry here keeps the
// algorithm and its digest together in one self-describing string.
const canonicalPackageRegistryArtifactUpsertCypher = `UNWIND $rows AS row
MERGE (a:PackageArtifact:PackageRegistryPackageArtifact {uid: row.uid})
SET a.id = row.uid,
    a.package_id = row.package_id,
    a.version_id = row.version_id,
    a.artifact_key = row.artifact_key,
    a.version = row.version,
    a.ecosystem = row.ecosystem,
    a.registry = row.registry,
    a.artifact_type = row.artifact_type,
    a.artifact_url = row.artifact_url,
    a.artifact_path = row.artifact_path,
    a.size_bytes = row.size_bytes,
    a.hashes = row.hashes,
    a.classifier = row.classifier,
    a.platform_tags = row.platform_tags,
    a.source_fact_id = row.source_fact_id,
    a.stable_fact_key = row.stable_fact_key,
    a.source_system = row.source_system,
    a.source_record_id = row.source_record_id,
    a.source_confidence = row.source_confidence,
    a.collector_kind = row.collector_kind,
    a.collector_instance_id = row.collector_instance_id,
    a.correlation_anchors = row.correlation_anchors,
    a.scope_id = row.scope_id,
    a.generation_id = row.generation_id,
    a.evidence_source = 'projector/package_registry'`

// canonicalPackageRegistryArtifactEdgeCypher is the deferred HAS_ARTIFACT edge
// MERGE, run in the second write group after the PackageVersion and
// PackageArtifact node phases commit.
const canonicalPackageRegistryArtifactEdgeCypher = `UNWIND $rows AS row
MATCH (v:PackageVersion {uid: row.version_id})
MATCH (a:PackageArtifact {uid: row.uid})
MERGE (v)-[rel:HAS_ARTIFACT]->(a)
SET rel.generation_id = row.generation_id,
    rel.evidence_source = 'projector/package_registry'`

// buildPackageRegistryArtifactStatements emits the PackageArtifact node-upsert
// statements for the main write group.
func (w *CanonicalNodeWriter) buildPackageRegistryArtifactStatements(
	mat projector.CanonicalMaterialization,
) []Statement {
	return packageRegistryBatchedStatements(
		canonicalPackageRegistryArtifactUpsertCypher,
		packageRegistryArtifactRows(mat),
		w.batchSize,
		"PackageRegistryPackageArtifact",
		canonicalPhasePackageRegistryArtifacts,
		mat,
	)
}

// buildPackageRegistryArtifactEdgeStatements emits the deferred HAS_ARTIFACT
// edge MERGEs that attach each PackageArtifact to its owning PackageVersion.
// It runs as a separate write group after the PackageVersion and
// PackageArtifact node phases commit, for the same NornicDB read-your-writes
// reason as the version and dependency edge phases in
// package_registry_edge_writer.go.
func (w *CanonicalNodeWriter) buildPackageRegistryArtifactEdgeStatements(
	mat projector.CanonicalMaterialization,
) []Statement {
	return packageRegistryBatchedStatements(
		canonicalPackageRegistryArtifactEdgeCypher,
		packageRegistryArtifactRows(mat),
		w.batchSize,
		"PackageRegistryArtifactEdge",
		canonicalPhasePackageRegistryArtifactEdges,
		mat,
	)
}

// packageRegistryArtifactRows converts the materialization's artifact rows
// into the parameter-map shape the upsert and edge Cypher both UNWIND. The
// same row shape serves both statements (the edge Cypher only reads uid,
// version_id, and generation_id, mirroring packageRegistryVersionRows'
// dual-purpose reuse for the HAS_VERSION edge).
func packageRegistryArtifactRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.PackageRegistryArtifacts))
	for _, row := range mat.PackageRegistryArtifacts {
		rows = append(rows, map[string]any{
			"uid":                   row.UID,
			"package_id":            row.PackageID,
			"version_id":            row.VersionID,
			"artifact_key":          row.ArtifactKey,
			"version":               row.Version,
			"ecosystem":             row.Ecosystem,
			"registry":              row.Registry,
			"artifact_type":         row.ArtifactType,
			"artifact_url":          row.ArtifactURL,
			"artifact_path":         row.ArtifactPath,
			"size_bytes":            row.SizeBytes,
			"hashes":                packageRegistryHashPairs(row.Hashes),
			"classifier":            row.Classifier,
			"platform_tags":         row.PlatformTags,
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

// packageRegistryHashPairs flattens an algorithm->digest map into a sorted
// "algorithm:digest" string list, or nil when empty. Neo4j/NornicDB node
// properties cannot hold a nested map, so this is the property-safe encoding
// that keeps each digest bound to its algorithm (unlike
// PackageRegistryVersionRow.Checksums, which the pre-existing
// checksumAlgorithms helper reduces to algorithm names only). A colon-bearing
// algorithm name is rejected before a row ever reaches this writer:
// DecodePackageRegistryPackageArtifact (sdk/go/factschema/decode_packageregistry.go,
// #5458) dead-letters it as input_invalid at decode time, so every row here
// is guaranteed to carry a colon-free algorithm name and the split stays
// unambiguous. The digest half is not separately validated (it is expected to
// be hex, which cannot contain ':', but that shape is not enforced today).
func packageRegistryHashPairs(hashes map[string]string) []string {
	if len(hashes) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(hashes))
	for algorithm, digest := range hashes {
		pairs = append(pairs, algorithm+":"+strings.TrimSpace(digest))
	}
	sort.Strings(pairs)
	return pairs
}
