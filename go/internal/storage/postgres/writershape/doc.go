// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package writershape persists the applied graph-writer shape version so a
// deployment retires stale generations exactly once per writer-semantics
// upgrade (issue #6868).
//
// Contract: the marker table is created idempotently; AppliedVersion
// reports 0 for an absent row; ClaimVersion converges concurrent starters
// on one winner (a newer version supersedes a stale claim, a same-version
// retry waits out the lease); MarkAppliedVersion advances the version only
// forward and runs only after the upgrade refinalize succeeds;
// ReleaseClaim clears a failed winner's claim for immediate retry. The
// upgrade sequence itself lives in recovery.EnsureGraphWriterShape; this
// package owns only the marker row it converges on.
package writershape
