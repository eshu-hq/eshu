// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package taghistory holds the bounded graph reads and the grant-binding page
// assembly behind GET /api/v0/images/tag-history (issues #5459 and #6564).
//
// It owns the two single-clause statements the route runs -- the image_ref
// anchored ContainerImageTagObservation read and the
// ContainerImage-[:BUILT_FROM]->Repository lookup that binds a scoped caller's
// page to its repository grant -- plus the refill loop that fills a
// grant-filtered page to `limit` VISIBLE rows and the continuation cursor that
// replaced the raw row offset. That cursor is reversible and unauthenticated,
// which is an OPEN defect with a replacement being designed, not an accepted
// limitation -- see Cursor's doc comment before changing it.
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
