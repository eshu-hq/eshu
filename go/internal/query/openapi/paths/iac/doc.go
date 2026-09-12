// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iac holds the OpenAPI 3.0 path fragments for the IaC-drift and
// replatforming routes (Issue #6060, lane C): Routes, Resources,
// TerraformConfigStateDrift, Replatforming, ReplatformingOwnership,
// ReplatformingSelectors, and ReplatformingRollups — each an exported JSON
// string constant that openapi.Spec concatenates into the published
// document.
//
// The replatforming_*.go fragments live here, not in a separate package,
// because every replatforming route is served by an IaCHandler method
// (go/internal/query/replatforming_*_handler.go): they are the same
// handler family as the rest of this package, not a distinct one.
//
// This package holds route-fragment data only, no handler logic. It MUST
// NOT import the parent openapi package: openapi/spec.go imports every
// paths/<family> leaf, so the reverse import is a cycle.
package iac
