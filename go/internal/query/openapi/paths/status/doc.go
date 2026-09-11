// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package status holds the OpenAPI 3.0 path fragments for Eshu's status,
// admin, and operator surfaces: pipeline/collector/ingester/index status
// and OpenAPI serving routes (routes.go), environment comparison
// (compare.go), admin mutations and queries (admin.go), answer narration
// (answer_narration.go), collector and collector-extraction readiness
// (collector_readiness.go and collector_extraction_readiness.go),
// governance (governance.go), hosted readiness (hosted_readiness.go),
// timeseries metrics (metrics.go), operations status (operations.go), the
// operator control plane (operator_control_plane.go), fact schema versions
// (fact_schema_version.go), and semantic-extraction status (semantic.go).
//
// This is the broadest leaf in the openapi/paths tree. Each file exports
// one JSON string constant that openapi/spec.go concatenates, in order,
// into the published OpenAPI document; collector_extraction_readiness.go
// additionally declares an unexported constant it splices into its own
// exported fragment. This package MUST NOT import the parent openapi
// package: spec.go imports this package, so the reverse direction is an
// import cycle.
package status
