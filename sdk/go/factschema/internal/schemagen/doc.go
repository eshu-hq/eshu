// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package schemagen generates the JSON Schema artifacts checked in under
// sdk/go/factschema/schema/, using github.com/invopop/jsonschema to reflect
// each fact kind's typed payload struct. It is internal: collectors and the
// reducer consume the generated schema files and the decode seam, never this
// generator.
package schemagen
