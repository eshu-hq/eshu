// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package boundedset provides one deterministic dedupe/sort/cap algorithm for
// bounding declared-evidence row sets that both a write path (persisting a
// bounded preview) and a read path (defensively re-bounding whatever was
// actually persisted, including legacy unbounded payloads written before a
// cap existed) need to apply identically. Sharing this single generic
// implementation is what lets a write-time cap and a read-time defensive cap
// stay in lockstep: both callers supply their own type's ordering/identity
// comparators, but the sort, adjacent-dedupe, and cap mechanics live in
// exactly one place.
package boundedset
