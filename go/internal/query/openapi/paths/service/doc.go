// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package service holds the OpenAPI 3.0 path fragments for the
// service-catalog and per-service intelligence-report routes (Issue #6060,
// lane C): Catalog and IntelligenceReport, each an exported JSON string
// constant that openapi.Spec concatenates into the published document.
//
// These were the only two exported path constants in the whole query
// package before the #6060 move; this leaf carries no other history.
//
// This package holds route-fragment data only, no handler logic. It MUST
// NOT import the parent openapi package: openapi/spec.go imports every
// paths/<family> leaf, so the reverse import is a cycle.
package service
