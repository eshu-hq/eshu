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
var ErrClaimRejected = errors.New("workflow claim rejected")
