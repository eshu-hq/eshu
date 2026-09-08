// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudinventory admits provider cloud-inventory source facts into the
// shared canonical cloud_resource_uid keyspace (issues #1997 and #1998).
//
// The admission path consumes aws_resource, gcp_cloud_resource, and
// azure_cloud_resource source facts for one scope generation, resolves each
// record's provider raw identity (AWS ARN, GCP Cloud Asset Inventory full
// resource name, Azure ARM resource id) through
// [github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory.ResolveProviderIdentity]
// into one stable uid, and persists reducer-owned canonical CloudResource
// read-model facts, one per admitted resource. Declared, applied, and observed
// evidence layers stay distinct so a provider observation never overwrites
// declared IaC truth. Blank, malformed, ambiguous, and unsupported identities
// are counted and surfaced, never fabricated into a uid.
//
// Tag, identity-policy, and resource-change evidence attach onto the admitted
// resource sharing their uid: none of them is identity, so a record whose uid
// was not admitted from resource evidence is dropped and can never fabricate
// a canonical resource.
//
// The package is graph-neutral: canonical graph node and edge projection, the
// multi-cloud drift join, and API/MCP readback are deferred follow-ups. It
// publishes no readiness phase and declares no NodesNotReady failure class.
//
// The exported surface is [CloudInventoryAdmissionDomainDefinition],
// [CloudInventoryAdmissionHandler], [PostgresCloudInventoryAdmissionWriter],
// [CloudInventoryRecord], [AdmittedCloudResource],
// [CloudInventoryAdmissionWrite], [CloudInventoryAdmissionWriteResult],
// [CloudInventoryEvidenceLoader], [CloudInventoryAdmissionWriter],
// [CloudTagEvidenceRecord], [CloudTagEvidenceLoader],
// [CloudIdentityPolicyEvidenceRecord], [CloudIdentityPolicyEvidence],
// [CloudIdentityPolicyEvidenceLoader], [CloudResourceChangeEvidenceRecord],
// [CloudResourceChangeEvidence], [CloudResourceChangeEvidenceLoader],
// [SourceLayer], and [ManagementOrigin]. This package never imports
// internal/reducer.
package cloudinventory
