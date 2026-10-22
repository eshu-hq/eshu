// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package catalog holds the GCP compute- and security-family constant leaves
// for the #6696 collector consolidation: CAI asset-type strings,
// gcp_cloud_relationship relationship types, full-resource-name prefixes, and
// path markers, grouped by family and consolidated verbatim out of the flat
// extractor_<type>.go files in the gcpcloud root.
//
// The package is a leaf on purpose. The gcpcloud root declares one extractor
// per asset type in the same package, so package-private constants declared
// beside any extractor are visible to every sibling file; moving a family
// into a subpackage therefore requires exporting it. This package imports
// nothing but the standard library (not even go/internal/facts: every value
// here is a string or int literal), so the root and every extractor group
// package can import it one-way without a cycle. It must never import the
// gcpcloud root, the extractor leaf, or any extractor group package.
//
// Values here are the collector's wire contract: CAI asset-type tokens and
// reducer relationship types. A move must never change a value. The export
// rule is mechanical — uppercase the first letter, preserve everything else
// (assetTypeComputeDisk becomes AssetTypeComputeDisk) — so each catalog
// identifier maps back to exactly one pre-consolidation gcpcloud identifier.
// The value-pinning test pins every consolidated value byte-identical to its
// pre-consolidation gcpcloud value.
package catalog
