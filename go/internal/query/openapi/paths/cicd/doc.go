// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cicd holds the OpenAPI 3.0 path fragments for Eshu's CI/CD
// run-correlation reads: the run-correlation list (routes.go) and its
// count/inventory aggregate (run_correlation_aggregate.go). It is the
// smallest leaf in the openapi/paths tree.
//
// Each file exports one JSON string constant that openapi/spec.go
// concatenates, in order, into the published OpenAPI document. This
// package MUST NOT import the parent openapi package: spec.go imports this
// package, so the reverse direction is an import cycle.
package cicd
