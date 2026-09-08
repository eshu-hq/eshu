// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package serviceevidence holds the pure service-evidence content
// extractors shared by the service evidence stayer in package query and the
// repository narrative overviews in package repository (Issue #6060,
// lane B): docs-route references, OpenAPI spec summaries (including `$ref`
// resolution through a caller-supplied resolver), and the loose YAML/JSON
// document accessors they are built from.
//
// The YAML/OpenAPI implementation lives here -- and not in querycontract --
// so the shared contract package stays dependency-neutral: every handler
// family imports querycontract, and none of them should inherit a
// content-parsing runtime through it. The portable evidence types and the
// SpecFileResolver port stay in querycontract; this leaf imports
// querycontract, never the reverse, and neither this leaf nor querycontract
// imports the query root or the handler families.
package serviceevidence
