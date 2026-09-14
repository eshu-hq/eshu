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
// windows, and the SEALED keyset continuation cursor that replaced the raw row
// offset. The cursor names a row key, not a row position, and it is encrypted
// with the deployment DEK, so a caller can neither read where a page stopped nor
// choose where the next one starts; the residue that leaves is counts only, on
// Cursor's doc comment, and it is disclosed on every caller-facing surface
// rather than only here.
//
// It imports only the standard library and querycontract (GraphQuery,
// RepositoryAccessFilter, StringVal/BoolVal), never the query root or
// secretcrypto: the DEK arrives through the Sealer interface, which
// *secretcrypto.Keyring satisfies. The query root keeps the HTTP handler:
// capability gating, selector parsing, the response envelope, and telemetry.
//
// Why the join runs in Go rather than in Cypher: a two-MATCH grant join
// returned zero rows on the pinned NornicDB build and on upstream v1.3.1 for a
// seed whose answer was two rows, while the two single-clause reads returned
// exactly the seeded edges on both. See
// docs/internal/evidence/6564-tag-history-grant-binding.md.
package taghistory
