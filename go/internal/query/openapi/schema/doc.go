// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package schema holds OpenAPI JSON Schema fragments shared between the
// openapi package's components block and the route fragments under
// openapi/paths/. It exists as its own package for one reason: spec.go in
// the parent package imports every family package under paths/, so a
// fragment needed by both a components_*.go file and a paths/<leaf> file
// cannot live in package openapi without an import cycle
// (paths/<leaf> -> openapi -> paths/<leaf>). Pulling the shared fragment
// down to this leaf, which imports neither side, breaks the cycle.
//
// Every exported identifier here is a raw JSON string fragment meant for
// string concatenation into a larger OpenAPI document; none of it is
// unmarshaled or executed. This package MUST NOT import openapi or any
// openapi/paths/<leaf> package.
package schema
