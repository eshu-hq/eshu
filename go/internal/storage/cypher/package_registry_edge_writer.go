// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// Package registry edges are written in a deferred second write group, separate
// from the node phases. NornicDB does not make a node created with multiple
// labels (e.g. PackageVersion:PackageRegistryPackageVersion) visible to a later
// same-transaction UNWIND-driven MATCH, so an inline `MATCH ... MERGE edge`
// against a node MERGE'd earlier in the same atomic transaction finds nothing.
// Splitting the edge MERGEs into a group that runs after the node group commits
// lets the MATCH resolve against committed, per-label-indexed nodes.
const (
	canonicalPhasePackageRegistryVersionEdges    = "package_registry_version_edges"
	canonicalPhasePackageRegistryDependencyEdges = "package_registry_dependency_edges"
)

const canonicalPackageRegistryVersionEdgeCypher = `UNWIND $rows AS row
MATCH (p:Package {uid: row.package_id})
MATCH (v:PackageVersion {uid: row.uid})
MERGE (p)-[rel:HAS_VERSION]->(v)
SET rel.generation_id = row.generation_id,
    rel.evidence_source = 'projector/package_registry'`

const canonicalPackageRegistryDependencyEdgeCypher = `UNWIND $rows AS row
MATCH (v:PackageVersion {uid: row.version_id})
MATCH (target:Package {uid: row.dependency_package_id})
MATCH (d:PackageDependency {uid: row.uid})
MERGE (v)-[declares:DECLARES_DEPENDENCY]->(d)
SET declares.generation_id = row.generation_id,
    declares.evidence_source = 'projector/package_registry'
MERGE (d)-[depends:DEPENDS_ON_PACKAGE]->(target)
SET depends.generation_id = row.generation_id,
    depends.evidence_source = 'projector/package_registry'`

// buildPackageRegistryVersionEdgeStatements emits the deferred HAS_VERSION edge
// MERGEs that attach each PackageVersion to its owning Package. It runs as a
// separate write group after the Package and PackageVersion node phases commit,
// because NornicDB does not make a node created with multiple labels visible to
// a later same-transaction UNWIND-driven MATCH. The edge rows reuse the version
// node row parameters; only package_id, uid, and generation_id are referenced.
func (w *CanonicalNodeWriter) buildPackageRegistryVersionEdgeStatements(
	mat projector.CanonicalMaterialization,
) []Statement {
	return packageRegistryBatchedStatements(
		canonicalPackageRegistryVersionEdgeCypher,
		packageRegistryVersionRows(mat),
		w.batchSize,
		"PackageRegistryVersionEdge",
		canonicalPhasePackageRegistryVersionEdges,
		mat,
	)
}

// buildPackageRegistryDependencyEdgeStatements emits the deferred
// DECLARES_DEPENDENCY and DEPENDS_ON_PACKAGE edge MERGEs. It runs as a separate
// write group after the PackageVersion, PackageDependency, and dependency-target
// Package node phases commit, for the same NornicDB read-your-writes reason as
// the version edge phase. The edge rows reuse the dependency node row parameters.
func (w *CanonicalNodeWriter) buildPackageRegistryDependencyEdgeStatements(
	mat projector.CanonicalMaterialization,
) []Statement {
	return packageRegistryBatchedStatements(
		canonicalPackageRegistryDependencyEdgeCypher,
		packageRegistryDependencyRows(mat),
		w.batchSize,
		"PackageRegistryDependencyEdge",
		canonicalPhasePackageRegistryDependencyEdges,
		mat,
	)
}
