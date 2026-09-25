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
// moved block. The change is Neo4j-only (neo4jRetiredUniqueConstraints), so
// the NornicDB fingerprint does not move and needs no predecessor.
const graphSchemaNeo4jPreRetiredNarrowConstraintsFingerprint = "dc9d1cfb57e5cc89f89f6af0cdd8b39842241badf6856742c4b290dbeb986b74"
