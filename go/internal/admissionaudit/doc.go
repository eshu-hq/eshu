// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admissionaudit compares reducer admission decisions, graph facts, and
// API/MCP readback against independent product-truth fixture intent.
//
// The package is a pure comparison helper. Callers collect reducer decisions,
// canonical graph observations, and bounded readback rows, then pass those plain
// data snapshots to Audit. It never opens databases, calls graph backends, or
// writes canonical truth; that keeps dogfood and CI audits deterministic and
// public-safe.
package admissionaudit
