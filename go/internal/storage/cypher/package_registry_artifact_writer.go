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
// PackageArtifact node phases commit. The PackageVersion MATCH pins
// package_id: row.package_id in addition to uid: row.version_id (#5820 P2
// review finding): a malformed-but-schema-valid artifact fact can carry a
// package_id that names package A while its version_id resolves to a version
// node genuinely owned by package B. Anchoring on uid alone would still MATCH
// that version and MERGE an edge from B's version to an artifact node that
// itself claims package_id A -- internally contradictory package graph truth.
// Requiring both properties on the same node means a mismatched pair finds no
// row (MATCH drops that UNWIND row, no edge written) instead of silently
// cross-attaching the artifact to the wrong package's version. row.package_id
// is the artifact's own claimed package_id (packageRegistryArtifactRows), and
// v.package_id is the value the PackageVersion node itself was written with
// (canonicalPackageRegistryVersionUpsertCypher's v.package_id = row.package_id
// in package_registry_canonical_writer.go), so the predicate only passes when
// the two facts agree on which package the version belongs to.
const canonicalPackageRegistryArtifactEdgeCypher = `UNWIND $rows AS row
MATCH (v:PackageVersion {uid: row.version_id, package_id: row.package_id})
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
// checksumAlgorithms helper reduces to algorithm names only).
//
// #5820 P2 review finding: an earlier version of this fix rejected a
// colon-bearing algorithm name at decode time
// (DecodePackageRegistryPackageArtifact,
// sdk/go/factschema/decode_packageregistry.go) to keep this split
// unambiguous. That silently narrowed the public
// package_registry.package_artifact.v1 contract -- the JSON Schema's
// hashes.additionalProperties accepts any string key, and the same schema
// version previously decoded these payloads without complaint -- without a
// major version bump or compatibility shim. This function now escapes both
// the algorithm and digest instead of rejecting either: packageRegistryEscapeHashSegment
// backslash-escapes every literal '\' and ':' in each segment before joining
// them with an unescaped ':' separator, so that separator is always the
// FIRST unescaped ':' in the encoded string, regardless of what either
// segment contains. packageRegistrySplitHashPair is the decode-side
// counterpart proving the round trip (see
// TestPackageRegistryHashPairsRoundTripsColonBearingAlgorithm); no production
// caller re-splits an encoded pair today (no consumer reads PackageArtifact.
// hashes back off the graph), but the pair exists so the encoding's
// reversibility is proven rather than assumed, and so a future reader has a
// correct counterpart ready to use.
func packageRegistryHashPairs(hashes map[string]string) []string {
	if len(hashes) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(hashes))
	for algorithm, digest := range hashes {
		pairs = append(pairs, packageRegistryEscapeHashSegment(algorithm)+":"+
			packageRegistryEscapeHashSegment(strings.TrimSpace(digest)))
	}
	sort.Strings(pairs)
	return pairs
}

// packageRegistryEscapeHashSegment backslash-escapes every literal '\' and
// ':' in raw so the segment can sit on either side of packageRegistryHashPairs'
// ':' separator without being mistaken for it. A segment containing neither
// character is returned unchanged (the common case: hex digests and
// conventional algorithm names like "sha256" never allocate).
func packageRegistryEscapeHashSegment(raw string) string {
	if !strings.ContainsAny(raw, `\:`) {
		return raw
	}
	var b strings.Builder
	b.Grow(len(raw) + 4)
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' || raw[i] == ':' {
			b.WriteByte('\\')
		}
		b.WriteByte(raw[i])
	}
	return b.String()
}

// packageRegistrySplitHashPair is the decode-side counterpart to
// packageRegistryHashPairs: it splits one encoded "algorithm:digest" string
// back into its original, unescaped algorithm and digest. Because
// packageRegistryEscapeHashSegment backslash-escapes every literal ':' in
// both segments, the separator is always the first ':' NOT preceded by an
// (unescaped) backslash, so this walks the string tracking escape state
// rather than using strings.SplitN. ok is false only for a pair with no
// unescaped ':' at all, which packageRegistryHashPairs never produces.
func packageRegistrySplitHashPair(pair string) (algorithm, digest string, ok bool) {
	var algorithmBuilder strings.Builder
	escaped := false
	for i := 0; i < len(pair); i++ {
		c := pair[i]
		switch {
		case escaped:
			algorithmBuilder.WriteByte(c)
			escaped = false
		case c == '\\':
			escaped = true
		case c == ':':
			return algorithmBuilder.String(), packageRegistryUnescapeHashSegment(pair[i+1:]), true
		default:
			algorithmBuilder.WriteByte(c)
		}
	}
	return "", "", false
}

// packageRegistryUnescapeHashSegment reverses packageRegistryEscapeHashSegment:
// a backslash always introduces the next byte literally, and every other byte
// passes through unchanged.
func packageRegistryUnescapeHashSegment(escaped string) string {
	if !strings.ContainsRune(escaped, '\\') {
		return escaped
	}
	var b strings.Builder
	b.Grow(len(escaped))
	skipNext := false
	for i := 0; i < len(escaped); i++ {
		if skipNext {
			b.WriteByte(escaped[i])
			skipNext = false
			continue
		}
		if escaped[i] == '\\' {
			skipNext = true
			continue
		}
		b.WriteByte(escaped[i])
	}
	return b.String()
}
