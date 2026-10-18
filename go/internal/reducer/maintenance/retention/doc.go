// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package retention prunes superseded source-generation history in
// bounded Postgres transactions.
//
// [Runner] drains eligible batches beside normal reducer intent
// processing: a pass that prunes loops immediately, a pass that finds
// nothing sleeps the poll interval, and a pass that finds candidates
// but prunes none retries soon with backoff bounded by the poll
// interval (#7398). Lock-held candidates stay invisible by design (the
// candidate SELECT uses SKIP LOCKED), so a lock-starved pass keeps the
// full sleep. A cycle the pruner refuses with [ErrKeyIndexUnavailable]
// reports failure reason key_index_unavailable and retries on its next
// poll; an operator rebuilds the index to clear it.
//
// When [Runner.OrphanSweeper] is set, [Runner.RunOnce] also runs the search
// index orphan census and sweep (#7427 PR2) after the prune returns, at most
// once per poll interval, and reports it on its own gauge, counter, phases
// and log line; a sweep failure is counted but does not fail the pass.
//
// When [Runner.SupersededSweeper] is set and
// [Policy.SearchIndexSupersededSweepEnabled] holds, [Runner.RunOnce] then
// runs the flagged superseded sweep (#8231) under its own once-per-interval
// cadence: index rows of still-existing superseded generations are removed
// in committed batches while their facts stay held, reported on the
// superseded rows-swept counter, its two phases, per-batch and per-run log
// lines; a sweep failure is counted but does not fail the pass.
package retention
