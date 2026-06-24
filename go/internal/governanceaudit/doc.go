// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package governanceaudit defines audit-safe hosted governance decision events.
//
// The package validates low-cardinality event types, actor classes, scope
// classes, decisions, reason codes, safe hashes, and service-principal tokens
// before an event can be aggregated for status or MCP readbacks. It deliberately
// does not persist events, emit telemetry, or accept raw principals, source
// identifiers, prompts, provider responses, credential handles, private URLs, or
// token values.
package governanceaudit
