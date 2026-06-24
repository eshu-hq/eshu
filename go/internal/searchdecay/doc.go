// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchdecay defines decay scoring for selected non-canonical evidence.
//
// The package applies bounded half-life policies to ranking metadata for
// explicitly eligible evidence classes. Canonical graph truth and admitted
// durable relationships are skipped rather than demoted. The package performs
// no database, graph, NornicDB, API, MCP, or OTEL I/O; callers must bridge
// Observation values to operator telemetry when live adapters use decay scores.
package searchdecay
