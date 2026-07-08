// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command payload-usage-manifest implements Contract System v1 §6
// enforcement gate 2 (docs/internal/design/contract-system-v1.md#6-enforcement-gates):
// a machine-readable manifest of which payload fields each typed-decoded fact
// kind's handlers actually read, derived from typed factschema.Decode* seams
// across reducer, projector, query, loader, relationships, and replay
// surfaces. Its gate fails when a handler reads a field no checked-in JSON
// Schema (sdk/go/factschema/schema/*.json) declares — the reverse break the
// forward-direction factschema-diff gate (issue #4569) cannot catch — and when
// a new raw payload read appears on the loader/relationships/replay ratchet.
//
// This command is a thin CLI wrapper; all derivation and comparison logic
// lives in the importable go/internal/payloadusage package so
// go/internal/reducer's own drift-lock test (TestPayloadUsageManifest) can
// invoke the same logic without importing a "package main".
//
// Usage:
//
//	payload-usage-manifest -mode generate [-out <path>]
//	payload-usage-manifest -mode gate
package main
