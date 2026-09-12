// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package catalog holds the OpenAPI 3.0 path fragments for Eshu's
// deployment-capability surfaces: capability maturity (capabilities.go),
// component extension listing and diagnostics
// (component_extensions.go), query playbooks (playbooks.go), and the
// surface inventory (surface_inventory.go) -- together, "what can this
// deployment do".
//
// Each file exports one JSON string constant that openapi/spec.go
// concatenates, in order, into the published OpenAPI document. This
// package MUST NOT import the parent openapi package: spec.go imports this
// package, so the reverse direction is an import cycle.
package catalog
