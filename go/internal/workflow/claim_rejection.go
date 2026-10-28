// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import "errors"

// ErrClaimRejected reports that a fenced claim mutation was rejected because
// the claim ID, fencing token, owner, or status no longer matches the row the
// worker holds. The store is the enforcement point; this package owns the
// sentinel so collectors can distinguish a fenced-away claim (nothing left to
// fail, drop the stale item and keep polling) from a store failure (surface
// loudly) without importing the store implementation. Storage backends must
// return this exact value, unwrapped or wrapped with %w, when their fencing
// guards match zero rows.
//
// Replay-ack semantics (#9428): a zero-row terminal mutation (complete,
// release, fail-retryable, fail-terminal) is NOT a rejection when it is an
// identical replay of an already-applied epoch — matching claim ID, fencing
// token, and owner with the work item still carrying that fencing token in the
// op's terminal status. The store acks it with nil: the state is already
// there, so a retry after server-commit/response-loss must not report a
// committed generation as fenced away. Heartbeat never acks: it writes no
// terminal state, so a post-terminal heartbeat stays rejected. A replay with a
// different op, owner, or fencing epoch (for example after a reclaim) stays
// rejected.
var ErrClaimRejected = errors.New("workflow claim rejected")
