// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package concurrentreplay provides a thread-safe wrapper around a
// single-threaded collector.Source, plus a concurrent Driver that drains it,
// so a recorded tape can be replayed by the Ifá P2 concurrent replay driver
// (design doc 4102, issue #4395, parent epic #4389).
//
// cassette.Source is single-goroutine per collector.Service by design: its
// Next has poll-restart semantics, and its internal scope cursor is
// unsynchronized state, so concurrent Next calls would race. Source wraps any
// collector.Source delegate — cassette.Source today, other single-threaded
// replay flavors later — behind a mutex so N concurrent replay workers can
// drain one recorded tape safely, converting the delegate's per-poll-restart
// contract into exactly-once delivery of each recorded generation across the
// lifetime of the wrapper.
//
// The mutex serializes tape HANDOUT, which is inherently sequential — one
// cursor over one recorded file, the same property schedulereplay.ScheduledWorkSource
// has. It does not serialize the contended production path; the expensive
// commit stage runs outside the lock, per caller. The one-shot drain latch is
// a semantic requirement to prevent the delegate's poll-restart from
// double-replaying the tape under concurrent callers, not a concurrency-defect
// workaround.
//
// Driver runs N concurrent workers against one Source, committing each
// generation through a collector.Committer and failing fast — canceling the
// other workers — on the first error from either the source or the
// committer. It replaces collector.Service's single poll-loop consumption of
// a collector.Source with a concurrent one; it does not build the
// fact_work_items fan-out or the reducer drain harness those later slices of
// #4395 still own.
//
// FactSliceSource is the collector.Source delegate for the git-collector
// replay path: collector-git is live-only and has no cassette tape format of
// its own, so git-derived facts replay from the fact_records rows a prior
// ingestion run already wrote, via a FactLoader (satisfied in production by
// postgres.FactStore.LoadFacts). Like cassette.Source, FactSliceSource is
// unsynchronized on its own; wrap it in NewSource before draining it with
// concurrent Driver workers.
package concurrentreplay
