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
//
// Actor classes are closed: NormalizeEvent rejects any value outside the
// ActorClass constants. ActorClassScopedToken names a scoped-token or
// OIDC-bearer caller and ActorClassBrowserSession names a cookie-authenticated
// dashboard session; both carry an actor identity, and an event that has none
// must use ActorClassAnonymous or ActorClassSystem instead. ActorClassOperator
// is reserved for a human asserting an identity through an SSO login; a cookie
// session acting on an identity mutation is ActorClassBrowserSession.
package governanceaudit
