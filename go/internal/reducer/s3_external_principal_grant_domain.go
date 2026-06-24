// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// DomainS3ExternalPrincipalGrantMaterialization projects metadata-only
// s3_external_principal_grant facts into canonical
// (:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal) graph truth. It
// gates on the existing cloud_resource_uid canonical-nodes phase for the source
// S3 bucket and creates only bounded ExternalPrincipal identities derived from
// exact public, AWS-account, AWS-ARN, or AWS-service principal metadata.
const DomainS3ExternalPrincipalGrantMaterialization Domain = "s3_external_principal_grant_materialization"
