// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package taghistory holds the bounded graph reads and the grant-binding page
// assembly behind GET /api/v0/images/tag-history (issues #5459 and #6564).
//
// It owns the single-clause statements the route runs -- three keyset forms of
// the image_ref-anchored ContainerImageTagObservation read plus the SKIP form an
// unscoped caller still pages with, and the
// ContainerImage-[:BUILT_FROM]->Repository lookup that binds a scoped caller's
// page to its repository grant -- plus the refill loop that fills a
// grant-filtered page to `limit` VISIBLE rows out of FIXED MaxLimit-sized raw
// windows, and the keyset continuation cursor that replaced the raw row offset.
// The cursor names a row key, not a row position, so there is nothing
// position-shaped for a caller to read or forge; the one residue that leaves is
// on Cursor's doc comment, and it is disclosed on every caller-facing surface
// rather than only here.
//
// It imports only the standard library and querycontract (GraphQuery,
// RepositoryAccessFilter, StringVal/BoolVal), never the query root. The query
// root keeps the HTTP handler: capability gating, selector parsing, the
// response envelope, and telemetry.
//
// Why the join runs in Go rather than in Cypher: a two-MATCH grant join
// returned zero rows on the pinned NornicDB build and on upstream v1.3.1 for a
// seed whose answer was two rows, while the two single-clause reads returned
// exactly the seeded edges on both. See
// docs/internal/evidence/6564-tag-history-grant-binding.md.
package taghistory
