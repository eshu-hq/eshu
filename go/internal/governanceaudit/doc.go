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
// Actor classes are closed on write: NormalizeEvent rejects any value outside
// the ActorClass constants, and the same holds for EventType, ScopeClass, and
// Decision. ActorClassScopedToken names a scoped-token or OIDC-bearer caller
// and ActorClassBrowserSession names a cookie-authenticated dashboard session;
// both carry an actor identity, and an event that has none must use
// ActorClassAnonymous or ActorClassSystem instead. ActorClassOperator is
// reserved for a human asserting an identity through an SSO login; a cookie
// session acting on an identity mutation is ActorClassBrowserSession.
//
// The read path is tolerant: NormalizeStoredEvent keeps a stored class this
// build does not know, provided it is a bounded lowercase token, and runs
// every other guard unchanged. During a rolling upgrade a newer pod can write a
// class an older pod's registry lacks; the older pod returns the row with its
// stored value rather than failing the whole audit page (#6574). UnknownEnums
// names the fields that held such a value so the reader can log them.
package governanceaudit
