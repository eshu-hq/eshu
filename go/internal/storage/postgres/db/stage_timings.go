// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"context"
	"sync/atomic"
	"time"
)

// ReaderStage names one guarded-reader stage whose time a request can account
// for. The set is closed: it never carries SQL text, error text, or user data,
// and a value outside it is ignored rather than recorded.
type ReaderStage int

// The four guarded-reader stages a request pays for. They mirror the
// reader_borrow, reader_identity, reader_replay, and business_query stages the
// guarded reader reports to its observer.
const (
	// ReaderStageBorrow is the wait for a reader permit and pooled connection.
	ReaderStageBorrow ReaderStage = iota
	// ReaderStageIdentity is the borrowed-session topology identity check.
	ReaderStageIdentity
	// ReaderStageReplay is the replica replay-position fence.
	ReaderStageReplay
	// ReaderStageBusinessQuery is the business SQL itself.
	ReaderStageBusinessQuery

	readerStageCount = int(ReaderStageBusinessQuery) + 1
)

// StageTimings is a goroutine-safe, fixed-size per-request accumulator of
// guarded-reader stage time. A stage can run more than once per request (the
// borrow stage runs once per fenced read, and a snapshot set borrows several
// connections), so each stage carries the SUM of its durations and the number
// of observations rather than a single latency. It holds two atomic counters
// per stage and never grows.
type StageTimings struct {
	nanos [readerStageCount]atomic.Int64
	count [readerStageCount]atomic.Int64
}

type stageTimingsKey struct{}

// WithStageTimings returns a child context carrying a fresh accumulator and
// the accumulator itself. The guarded reader adds each stage it observes under
// that context, so the caller can attribute one request's reader time to
// borrow, identity, replay, and business query after the read returns.
func WithStageTimings(ctx context.Context) (context.Context, *StageTimings) {
	timings := &StageTimings{}
	return context.WithValue(ctx, stageTimingsKey{}, timings), timings
}

// StageTimingsFrom returns the accumulator WithStageTimings attached to ctx,
// or nil when the request has none. The lookup is one context value read.
func StageTimingsFrom(ctx context.Context) *StageTimings {
	timings, _ := ctx.Value(stageTimingsKey{}).(*StageTimings)
	return timings
}

// Add records one observation of stage lasting d. It is safe to call
// concurrently, is a no-op on a nil receiver or a stage outside the closed
// set, and clamps a negative duration to zero (the observation still counts).
func (t *StageTimings) Add(stage ReaderStage, d time.Duration) {
	if t == nil || stage < 0 || int(stage) >= readerStageCount {
		return
	}
	if d < 0 {
		d = 0
	}
	t.nanos[stage].Add(int64(d))
	t.count[stage].Add(1)
}

// Seconds returns the summed duration of every observation of stage, in
// seconds. It is zero for a nil receiver or a stage outside the closed set.
func (t *StageTimings) Seconds(stage ReaderStage) float64 {
	if t == nil || stage < 0 || int(stage) >= readerStageCount {
		return 0
	}
	return time.Duration(t.nanos[stage].Load()).Seconds()
}

// Count returns how many observations of stage were added. It is zero for a
// nil receiver or a stage outside the closed set.
func (t *StageTimings) Count(stage ReaderStage) int64 {
	if t == nil || stage < 0 || int(stage) >= readerStageCount {
		return 0
	}
	return t.count[stage].Load()
}

// Recorded reports whether any in-set stage was observed. A request whose read
// never went through the guarded reader leaves it false, so a caller can omit
// the stage attributes instead of logging misleading zeros.
func (t *StageTimings) Recorded() bool {
	if t == nil {
		return false
	}
	for i := range t.count {
		if t.count[i].Load() > 0 {
			return true
		}
	}
	return false
}
