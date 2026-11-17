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
//
// When [Runner.RelationshipSweeper] is set, [Runner.RunOnce] finally runs the
// relationship retention census and sweep (#10823) after the superseded sweep,
// outside the prune transaction. The read-only census, and the orphan gauge it
// sets, run every time the sweep is due; the deletes of relationship evidence,
// candidate and generation rows run only when
// [Policy.RelationshipRetentionSweepEnabled] holds (off by default), bounded by
// [Policy.RelationshipRetentionRowCap] and a quarter of the poll interval.
// The sweep is due once per poll interval, or once per sixth of it after a pass
// the row cap, deadline or census list limit stopped; a failed pass, a clean
// pass and a pass that skipped a batch on a lock timeout keep the full
// interval. While the latest pass was capped, [Runner.Run] also holds its idle
// wait and its skipped-retry wait to at most a sixth of the poll interval, so a
// relationship backlog shortens the prune's own waits as well as the sweep's;
// the search index sweeps keep their own once-per-poll due checks. A prune that
// fails ([Runner.RunOnce] returns an error) waits the full poll interval even
// straight after a capped relationship pass. A sweep failure is counted but does
// not fail the pass.
package retention
