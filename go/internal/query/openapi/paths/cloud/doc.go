// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloud holds the OpenAPI 3.0 path fragments for the cloud
// inventory and cloud/AWS runtime-drift routes (Issue #6060, lane C):
// Routes, Inventory, RuntimeDrift, and AWSRuntimeDrift, each an exported
// JSON string constant that openapi.Spec concatenates into the published
// document.
//
// AWSRuntimeDrift shares the runtime-drift domain with RuntimeDrift but
// documents the AWS-specific route; openapi/spec.go concatenates it apart
// from Routes+Inventory+RuntimeDrift, so file order here does not match
// assembly order there.
//
// This package holds route-fragment data only, no handler logic. It MUST
// NOT import the parent openapi package: openapi/spec.go imports every
// paths/<family> leaf, so the reverse import is a cycle.
package cloud
