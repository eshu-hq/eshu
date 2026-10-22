// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package catalog holds the consolidated GCP collector string constants for
// issue #6696: CAI asset-type strings, gcp_cloud_relationship relationship
// types, full-resource-name prefixes, and path markers, grouped by family
// and consolidated verbatim out of the flat extractor files in the gcpcloud
// root. Leaf files: gcp_compute_core.go, gcp_compute_lb.go, and
// gcp_compute_net.go (compute family), gcp_security.go (security family),
// gcp_data.go (data family: AlloyDB, BigQuery, Bigtable, Dataflow, Dataform,
// Dataplex, Dataproc, Filestore, Firestore, Memorystore, Spanner, Cloud SQL),
// gcp_platform.go (platform family: Artifact Registry, Cloud Build,
// Cloud DNS, Firebase, GKE, Logging, Pub/Sub, Cloud Storage),
// gcp_shared.go (shared extractor asset types, resource-name prefixes,
// sentinels, and builders from the extractor leaf, the claim Boundary and
// ParentScopeKind shapes, the relationship support states, the warning kinds
// and outcomes, the claim/extraction status enums, the collector kind, the
// redaction policy version and reasons, and the observation bounds), and
// gcp_runtime.go (the live-client and tag-provider runtime bounds and the
// freshness event kinds and trigger states).
//
// The package is a leaf on purpose. The gcpcloud root declares one extractor
// per asset type in the same package, so package-private constants declared
// beside any extractor are visible to every sibling file; moving a family
// into a subpackage therefore requires exporting it. The shared, root, and
// runtime constants are needed by the root, every extractor file, and the
// gcpruntime and freshness subpackages at once, so they live in this same
// leaf. This package imports nothing but the standard library (time for the
// live-request and poll bounds; every other value is a string, int, or
// duration literal), so the root and every extractor group package can
// import it one-way without a cycle. It must never import the gcpcloud root,
// the extractor leaf, or any extractor group package.
//
// Values here are the collector's wire contract: CAI asset-type tokens,
// reducer relationship types, full-resource-name prefixes, bounded enums
// safe for telemetry labels, live API endpoints and bounds, and redaction
// reasons. A move must never change a value. The export rule is mechanical
// — uppercase the first letter, preserve everything else
// (assetTypeComputeDisk becomes AssetTypeComputeDisk) — so each catalog
// identifier maps back to exactly one pre-consolidation gcpcloud identifier.
// The value-pinning tests pin every consolidated value byte-identical to its
// pre-consolidation gcpcloud value.
package catalog
