// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package freshness holds the OpenAPI 3.0 path fragments for Eshu's
// freshness and change-tracking reads: the freshness causality doc
// (causality.go), repo-level changed-since (changed_since.go), generation
// tracking (generations.go), and service-level changed-since
// (service_changed_since.go).
//
// Each file exports one JSON string constant that openapi/spec.go
// concatenates, in order, into the published OpenAPI document. This
// package MUST NOT import the parent openapi package: spec.go imports this
// package, so the reverse direction is an import cycle.
package freshness
