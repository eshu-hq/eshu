// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gcpcloud builds durable source facts for the GCP cloud collector from
// Cloud Asset Inventory (CAI) responses.
//
// The package is fixture-driven and provider-agnostic at the transport layer:
// callers parse CAI assets.list or searchAllResources JSON into observations and
// the package normalizes, redacts, and emits gcp_cloud_resource,
// gcp_cloud_relationship, gcp_tag_observation, gcp_iam_policy_observation,
// gcp_dns_record, gcp_image_reference, and gcp_collection_warning facts. From the
// same IAM bindings it also emits the secrets/IAM mirror (gcp_iam_principal,
// gcp_iam_trust_policy, gcp_iam_permission_policy) for service-account grantees
// and ServiceAccount impersonation bindings so the reducer can correlate GCP IAM
// into the secrets/IAM read models (#2347/#2369). It
// never calls Google Cloud APIs, never writes graph truth, and never persists raw
// IAM policy bodies, DNS record values, environment variable values, secret
// values, object contents, public or private IP addresses, startup scripts, or
// other data-plane records. Reducers own canonical
// CloudResource identity, drift, relationship edges, and API/MCP truth.
//
// The durable claim boundary is explicit: collector instance, parent scope kind
// and id, asset and content family, location bucket, scope id, generation id,
// and a positive fencing token. Raw provider identity (the CAI full resource
// name) is preserved verbatim for exact reducer joins alongside normalized asset
// type, project id/number, folder and organization ancestors, and location.
// Stable fact keys derive from fact kind, full resource name, asset type,
// content family, and provider update time so duplicate delivery converges and a
// stale generation is rejected rather than replacing current facts.
//
// Per-asset-type typed depth is captured through an extractor registry: each
// supported CAI asset type registers an AssetAttributeExtractor in its own file
// (RegisterAssetExtractor), and the parser hands that extractor the raw
// resource.data blob transiently so it can return a bounded, redaction-safe
// attributes map, cross-source correlation anchors, and typed provider
// relationships. The parser remains the single redaction choke point: the raw
// blob never leaves it, and only the extractor's safe output reaches the
// gcp_cloud_resource attributes/correlation_anchors fields. BigQuery Table
// (bigquery.googleapis.com/Table) is the reference extractor.
//
// This GCP collector slice covers resource inventory, provider relationship
// observations, label-backed and opt-in direct/effective tag observations, IAM
// policy observations, DNS record observations, Cloud Run runtime image-reference
// observations, collection-warning evidence, and the scoped telemetry contract.
// The sanitized live smoke gate is a documented follow-up. See
// docs/public/reference/gcp-cloud-collector-contract.md for the full contract.
package gcpcloud
