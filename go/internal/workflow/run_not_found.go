// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import "errors"

// ErrRunNotFound reports that no workflow run row exists for the requested
// run ID. The store is the enforcement point; this package owns the sentinel
// so claim-time transport readers can distinguish a missing run row (fail
// the claim closed: retry cannot produce queries for a run row that has
// none) from a store-transport fault such as a connection reset or statement
// timeout (fail retryable: the claim plane must retry the claim) without
// importing the store implementation. Storage backends must return this
// exact value, unwrapped or wrapped with %w, when their run lookup matches
// zero rows.
var ErrRunNotFound = errors.New("workflow run not found")
