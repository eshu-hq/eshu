// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package repository holds the OpenAPI 3.0 path fragments for the
// repository, package-registry, and dependency routes (Issue #6060,
// lane C): Routes, Branches, Freshness, StatsAndCoverage, Dependencies,
// PackageRegistry, and PackageRegistryAggregate — each an exported JSON
// string constant that openapi.Spec concatenates into the published
// document.
//
// Routes imports openapi/schema for the shared evidence_boundaries
// fragment (schema.EvidenceBoundaries); every other constant in this
// package is self-contained.
//
// This package holds route-fragment data only, no handler logic. It MUST
// NOT import the parent openapi package: openapi/spec.go imports every
// paths/<family> leaf, so the reverse import is a cycle.
package repository
