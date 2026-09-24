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

// graphSchemaNeo4jPreUnconstrainedUIDIndexFingerprint and its NornicDB peer are
// the digests immediately before the #7057 rationale_uid and
// documentation_section_uid indexes were added (the #6793 tip above). The bump
// is additive and lists them as compatible: both writers already MERGE
// Rationale and DocumentationSection on uid, and the indexes change no MERGE or
// MATCH identity, so a writer on the previous schema writes exactly the same
// graph. The indexes let the Neo4j entity-id anchor seek those uids, and put
// the writers' MERGE on an index lookup instead of a label scan.
const (
	graphSchemaNeo4jPreUnconstrainedUIDIndexFingerprint    = "9041fb74aae9f09afe78b4dacbd7b4b64a619e172ac0a107ec45dcea8f566717"
	graphSchemaNornicDBPreUnconstrainedUIDIndexFingerprint = "f957752df4f6114440959c6a48162d7a192e98c724918abfde897b8fd67ecef4"
)
