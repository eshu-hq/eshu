// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"testing"
	"time"
)

// WAL-accounting flush-race regression (#7371 follow-up). Trial DEFAULT_5
// measured the race signature on a 702ms prune run: wal_bytes=0,
// wal_records=0, wal_fpi=0 while lsn_bytes=15424952 proved 15.4MB of WAL was
// inserted. Runs past ~1s always show full counts (DEFAULT_1, CKPT_A/B,
// FILE_COPY_2/4/6). The counters lag because each backend flushes its pending
// statistics to shared memory just before going idle, at most once per
// PGSTAT_MIN_INTERVAL (1s); pg_current_wal_insert_lsn() is exact at call
// time. The harness must therefore wait out the throttle window when the
// after-prune sample shows inserted-but-unaccounted WAL, instead of printing
// the stale zeros.

func TestWALFlushSettledDecision(t *testing.T) {
	before := walStats{records: 100, fpi: 10, bytes: 1000, lsn: 5000}
	cases := []struct {
		name    string
		after   walStats
		settled bool
	}{
		// Trial DEFAULT_5 shape: 15.4MB inserted, zero counters.
		{name: "race signature waits", after: walStats{records: 100, fpi: 10, bytes: 1000, lsn: 5000 + 15424952}, settled: false},
		// Live micro-repro shape: 74.5MB inserted, only 50 background bytes
		// flushed. A bare bytes>before check would wrongly settle here.
		{name: "background noise waits", after: walStats{records: 100, fpi: 10, bytes: 1000 + 50, lsn: 5000 + 74532208}, settled: false},
		// Trial DEFAULT_1 shape: counters account for the inserted WAL.
		{name: "flushed counters settle", after: walStats{records: 100 + 270495, fpi: 10 + 16554, bytes: 1000 + 146299860, lsn: 5000 + 147313304}, settled: true},
		// Trial DEFAULT_3 shape: 96.2% agreement on a clean sub-second run.
		{name: "near-full fast run settles", after: walStats{records: 100 + 270490, fpi: 10 + 1, bytes: 1000 + 14839171, lsn: 5000 + 15424952}, settled: true},
		// Nothing material inserted: there is no WAL worth waiting for.
		{name: "idle cluster settles", after: before, settled: true},
		{name: "tiny run settles", after: walStats{records: 100, fpi: 10, bytes: 1000, lsn: 5000 + 1024}, settled: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := walFlushSettled(before, tc.after); got != tc.settled {
				t.Fatalf("walFlushSettled(before, %+v) = %v, want %v", tc.after, got, tc.settled)
			}
		})
	}
}

func TestSettleWALStatsRetriesRaceThenReturnsFlushed(t *testing.T) {
	before := walStats{bytes: 1000, lsn: 5000}
	stale := walStats{bytes: 1000 + 50, lsn: 5000 + 74532208}
	flushed := walStats{bytes: 1000 + 74000073, lsn: 5000 + 74448576}
	samples := []walStats{stale, stale, flushed}
	var polls, sleeps int
	got := settleWALStats(before, func() walStats {
		polls++
		return samples[min(polls-1, len(samples)-1)]
	}, func(time.Duration) { sleeps++ })
	if got != flushed {
		t.Fatalf("settleWALStats = %+v, want flushed %+v", got, flushed)
	}
	if polls != 3 || sleeps != 2 {
		t.Fatalf("settleWALStats polled %d times with %d sleeps, want 3 polls and 2 sleeps", polls, sleeps)
	}
}

func TestSettleWALStatsCleanSampleReturnsImmediately(t *testing.T) {
	before := walStats{bytes: 1000, lsn: 5000}
	after := walStats{bytes: 2000, lsn: 6000}
	var polls, sleeps int
	got := settleWALStats(before, func() walStats {
		polls++
		return after
	}, func(time.Duration) { sleeps++ })
	if got != after || polls != 1 || sleeps != 0 {
		t.Fatalf("settleWALStats = %+v after %d polls/%d sleeps, want immediate return with 1 poll/0 sleeps", got, polls, sleeps)
	}
}

func TestSettleWALStatsBoundExpires(t *testing.T) {
	before := walStats{bytes: 1000, lsn: 5000}
	stale := walStats{bytes: 1000, lsn: 5000 + 15424952}
	var polls int
	got := settleWALStats(before, func() walStats {
		polls++
		return stale
	}, func(time.Duration) {})
	if got != stale {
		t.Fatalf("settleWALStats = %+v, want last sample %+v", got, stale)
	}
	if polls != 1+maxWALSettlePolls {
		t.Fatalf("settleWALStats polled %d times, want bound 1+%d", polls, maxWALSettlePolls)
	}
}
