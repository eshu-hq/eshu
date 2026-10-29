// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package support serves the CLI-support endpoints: pure core logic
// over client-supplied artifacts for the extracted thin CLI (8874 S4).
//
// Each route wraps one local-only function the CLI cannot carry after
// extraction: investigation packet rendering, service-report composition,
// report bundle capture and validation, and answer-quality scoring. Every
// handler is a pure function of its request body — no graph, Postgres,
// collector, or queue reads — so the package has zero construction
// dependencies and mounts additively in go/cmd/api wiring.
//
// The wire rules live in docs/internal/evidence/8874-s4-endpoint-decisions.md:
// bounded bodies, strict single-object decoding (unknown fields and trailing
// values fail with 400), per-route required-field checks, envelope errors,
// differential parity with the local functions, and no MCP tools (these are
// CLI-support endpoints, not user-facing query capabilities).
package support
