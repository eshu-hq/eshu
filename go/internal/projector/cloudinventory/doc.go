// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudinventory builds the cloud-inventory-admission reducer intent
// from one immutable scope generation: when the generation carries at least
// one provider cloud-inventory source fact — an aws_resource,
// gcp_cloud_resource, or azure_cloud_resource — it asks the reducer to admit
// that provider evidence into the shared canonical CloudResource identity
// keyspace once, so GET /api/v0/cloud/inventory returns rows (#2209). The
// anchor is the earliest source fact in original input order across the three
// kinds — there is no per-kind priority — so the reducer claim is stable
// across reprojections of the same generation. Only the fact kind is read; no
// payload is decoded, and schema-version admission stays with root
// projection, which rejects an unsupported provider schema version before any
// builder runs. The source-system label is the shared two-tier
// projectorintent.SourceSystem fallback (SourceRef, then CollectorKind); the
// root file's private helper was a pure delegation to it, so the child
// carries no helper of its own. The reducer's DomainCloudInventoryAdmission
// handler owns candidate classification and every identity-row write. Root
// projector assembly owns lookup construction and lifetime, invocation order,
// queue writes, retries, and telemetry.
package cloudinventory
