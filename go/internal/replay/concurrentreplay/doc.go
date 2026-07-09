// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package concurrentreplay provides a thread-safe wrapper around a
// single-threaded collector.Source so it can be driven by the Ifá P2
// concurrent replay driver (design doc 4102, issue #4395, parent epic #4389).
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
package concurrentreplay
