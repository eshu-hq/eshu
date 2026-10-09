// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

// graphSchemaNeo4jPreInfraEvidenceSourceIndexFingerprint and its NornicDB peer
// are the digests immediately before the #6793 tf_module_evidence_source and
// tf_output_evidence_source indexes were added (the #6541 directory_repo_id
// index tip). The bump is additive and lists them as compatible: the indexes
// back the infra resource aggregate's graph read of the Terraform state
// projector's TerraformModule / TerraformOutput nodes, and change no MERGE or
// MATCH identity, so a writer on the previous schema writes exactly the same
// graph. It merely leaves that read a label scan instead of a seek.
const (
	graphSchemaNeo4jPreInfraEvidenceSourceIndexFingerprint    = "5483f897a164b79bae02246b237351a3924481b86a3bb35ddaee5b0d673cc5f0"
	graphSchemaNornicDBPreInfraEvidenceSourceIndexFingerprint = "5ca5fcafda58ff9bc825e5bbf4196834282cff318919fabdd18eb625ebeaea0d"
)

// graphSchemaNeo4jPreUnconstrainedUIDIndexFingerprint is the Neo4j digest
// immediately before the #7057 rationale_uid and documentation_section_uid
// indexes were added (the #6793 tip above). The bump is additive and lists it
// as compatible: the indexes change no MERGE or MATCH identity, so a writer on
// the previous schema writes exactly the same graph; they let the Neo4j
// entity-id anchor seek those uids. The indexes are Neo4j-only
// (neo4jUIDLookupIndexes), so the NornicDB fingerprint does not move and needs
// no predecessor.
const graphSchemaNeo4jPreUnconstrainedUIDIndexFingerprint = "9041fb74aae9f09afe78b4dacbd7b4b64a619e172ac0a107ec45dcea8f566717"

// graphSchemaNeo4jPreRetiredNarrowConstraintsFingerprint is the Neo4j digest
// immediately before #7095 retired the uniqueness constraints narrower than the
// canonical uid identity (the #7057 tip above). The bump lists it as
// compatible: it drops constraints and adds three non-unique path indexes, and
// changes no MERGE or MATCH identity, so a writer on the previous schema writes
// exactly the same graph against the new one -- and no longer dead-letters a
// moved block. The Neo4j change is Neo4j-only (neo4jRetiredUniqueConstraints);
// NornicDB's own retirement (#7097) has its predecessor below.
const graphSchemaNeo4jPreRetiredNarrowConstraintsFingerprint = "dc9d1cfb57e5cc89f89f6af0cdd8b39842241badf6856742c4b290dbeb986b74"

// graphSchemaNornicDBPreRetiredNarrowConstraintsFingerprint is the NornicDB
// digest immediately before #7097 retired the single-property uniqueness
// constraints narrower than the canonical uid identity (kustomize_unique,
// helm_values_unique, tg_config_unique; the NornicDB tip after #6793). The bump
// lists it as compatible: it only drops constraints and adds no statement that
// changes a MERGE or MATCH identity, so a writer on the previous schema writes
// exactly the same graph against the new one -- and no longer retries a moved
// block on a UNIQUE violation. It adds no path index, because the retract's
// path IN $file_paths filter, combined with the repo, evidence-source and
// generation predicates, does not seek one on NornicDB.
const graphSchemaNornicDBPreRetiredNarrowConstraintsFingerprint = "f957752df4f6114440959c6a48162d7a192e98c724918abfde897b8fd67ecef4"

// graphSchemaNeo4jPreFulltextModernFormFingerprint is the Neo4j digest
// immediately before #7675 made the Neo4j dialect execute the modern CREATE
// FULLTEXT INDEX form instead of the db.index.fulltext.createNodeIndex
// procedure Neo4j removed in 5.0 (the #7095 retirement tip). The bump is
// additive and lists it as compatible: the two indexes keep their names,
// labels, and properties, so a writer on the previous schema writes exactly
// the same graph and readers query the same index names; the change only
// stops attempting a procedure call the pinned backend always rejects.
// NornicDB still executes the procedure form, so its fingerprint does not
// move and needs no predecessor.
const graphSchemaNeo4jPreFulltextModernFormFingerprint = "675dafc901ff633999440f0e02cbefa7c3ff80231832f5a67330aafe565f8d54"

// graphSchemaCompatibleFingerprints lists additive predecessor schema
// fingerprints that older graph writers may safely use after bootstrap records
// the current marker. The key is the schema fingerprint that was applied; the
// value lists predecessor fingerprints whose writers stay compatible with it.
// Destructive schema changes, schema changes coupled to new reducer domains,
// and write-identity cutovers must not list predecessors.
//
// The current fingerprint lists only the additive index bumps made since the
// #6102 Module (name, lang) cutover; the chain stops at that cutover, because
// it changed what a writer MERGEs on and a writer on any earlier release
// resolves an import-edge target by module name alone. The pre-cutover entry
// below is retained, keyed by that schema's own fingerprint rather than the
// current one, so it can never be reached by the current lookup. It records
// what that schema admitted, and the fence tests drive the real admission
// decision with it.
var graphSchemaCompatibleFingerprints = map[SchemaBackend]map[string][]string{
	SchemaBackendNeo4j: {
		graphSchemaNeo4jFingerprint: {
			graphSchemaNeo4jPreFulltextModernFormFingerprint,
			graphSchemaNeo4jPreRetiredNarrowConstraintsFingerprint,
			graphSchemaNeo4jPreUnconstrainedUIDIndexFingerprint,
			graphSchemaNeo4jPreInfraEvidenceSourceIndexFingerprint,
			graphSchemaNeo4jPreDirectoryRepoIDIndexFingerprint,
		},
		graphSchemaNeo4jPreModuleIdentityFingerprint: {
			graphSchemaNeo4jPreRegistryEventFingerprint,
			graphSchemaNeo4jPreArtifactFingerprint,
			graphSchemaNeo4jPreKubernetesNamespaceIndexesFingerprint,
			graphSchemaNeo4jPreKustomizeOverlayRepoIDIndexFingerprint,
			graphSchemaNeo4jPreTerraformStateResourceAddressIndexFingerprint,
			graphSchemaNeo4jPreTerraformStateResourceIndexesFingerprint,
			graphSchemaNeo4jPreTerraformStateResourceSplitFingerprint,
			graphSchemaNeo4jPreCodeownersOwnershipFingerprint,
			graphSchemaNeo4jPreFluxHelmEntitiesFingerprint,
			graphSchemaNeo4jPreFluxTypedEntitiesFingerprint,
			graphSchemaNeo4jPreSqlMigrationFingerprint,
			graphSchemaNeo4jPreShellExecRetractIndexesFingerprint,
			graphSchemaNeo4jPreInheritanceRetractIndexesFingerprint,
			graphSchemaNeo4jPreFunctionRetractIndexesFingerprint,
			graphSchemaNeo4jPreHelmTemplateValuesFingerprint,
			graphSchemaNeo4jPreGitlabFingerprint,
			graphSchemaNeo4jPreContentEntityGraphFingerprint,
		},
	},
	SchemaBackendNornicDB: {
		graphSchemaNornicDBFingerprint: {
			graphSchemaNornicDBPreRetiredNarrowConstraintsFingerprint,
			graphSchemaNornicDBPreInfraEvidenceSourceIndexFingerprint,
			graphSchemaNornicDBPreDirectoryRepoIDIndexFingerprint,
		},
		graphSchemaNornicDBPreModuleIdentityFingerprint: {
			graphSchemaNornicDBPreRegistryEventFingerprint,
			graphSchemaNornicDBPreArtifactFingerprint,
			graphSchemaNornicDBPreKubernetesNamespaceIndexesFingerprint,
			graphSchemaNornicDBPreKustomizeOverlayRepoIDIndexFingerprint,
			graphSchemaNornicDBPreTerraformStateResourceAddressIndexFingerprint,
			graphSchemaNornicDBPreTerraformStateResourceIndexesFingerprint,
			graphSchemaNornicDBPreTerraformStateResourceSplitFingerprint,
			graphSchemaNornicDBPreCodeownersOwnershipFingerprint,
			graphSchemaNornicDBPreKubernetesWorkloadIDLookupFingerprint,
			graphSchemaNornicDBPreFluxHelmEntitiesFingerprint,
			graphSchemaNornicDBPreFluxTypedEntitiesFingerprint,
			graphSchemaNornicDBPreSqlMigrationFingerprint,
			graphSchemaNornicDBPreFunctionLegacyIDLookupFingerprint,
			graphSchemaNornicDBPreShellExecRetractIndexesFingerprint,
			graphSchemaNornicDBPreInheritanceRetractIndexesFingerprint,
			graphSchemaNornicDBPreFunctionRetractIndexesFingerprint,
			graphSchemaNornicDBPreHelmTemplateValuesFingerprint,
			graphSchemaNornicDBPreGitlabFingerprint,
			graphSchemaNornicDBPreContentEntityGraphFingerprint,
		},
	},
}
