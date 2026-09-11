// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidence holds the pure service-evidence content extractors
// shared by the service evidence stayer in package query, the service
// evidence family in package service, and the repository narrative
// overviews in package repository (Issue #6060, lane B; nested under
// service/ for #6642 Part D): docs-route references, OpenAPI spec summaries
// (including `$ref` resolution through a caller-supplied resolver), and the
// loose YAML/JSON document accessors they are built from.
//
// The YAML/OpenAPI implementation lives here -- and not in querycontract --
// so the shared contract package stays dependency-neutral: every handler
// family imports querycontract, and none of them should inherit the
// `gopkg.in/yaml.v3` content-parsing runtime through it. The portable
// evidence types and the SpecFileResolver port stay in querycontract; this
// leaf imports the Go standard library, `gopkg.in/yaml.v3`, and
// querycontract, never the reverse. It never imports the query root or any
// handler family, including its own parent package service.
package evidence
