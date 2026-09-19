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
