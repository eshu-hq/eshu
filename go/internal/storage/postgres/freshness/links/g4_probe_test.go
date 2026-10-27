// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// g4ControlInserts is the number of single-row autocommit inserts the durable
// commit control times before the gated link.
const g4ControlInserts = 5

// postgresSettings reads the three settings that decide how a commit waits
// for the disk and how much of the fixture stays in memory.
func (l *ledgerDB) postgresSettings(t *testing.T) string {
	t.Helper()
	var parts []string
	for _, name := range []string{"fsync", "synchronous_commit", "shared_buffers"} {
		var value string
		if err := l.raw.QueryRowContext(l.ctx, `SHOW `+name).Scan(&value); err != nil {
			t.Fatalf("SHOW %s: %v", name, err)
		}
		parts = append(parts, name+"="+value)
	}
	return strings.Join(parts, " ")
}

// durableCommitControl times g4ControlInserts single-row autocommit inserts,
// each its own transaction with its own WAL flush, into a scratch table. It
// is a regular table on purpose: a temporary or unlogged table writes no WAL,
// so its commits would never wait for a flush and could not show a slow one.
// The control is logged next to the gated link and never gated itself. If it
// spikes when the link does, commit latency of the host is the cause; if the
// link spikes alone, it is not.
func (l *ledgerDB) durableCommitControl(t *testing.T) []time.Duration {
	t.Helper()
	l.exec(t, `CREATE TABLE g4_commit_control (id int PRIMARY KEY, note text NOT NULL)`)
	conn, err := l.raw.Conn(l.ctx)
	if err != nil {
		t.Fatalf("control conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	timings := make([]time.Duration, 0, g4ControlInserts)
	for i := range g4ControlInserts {
		start := time.Now()
		if _, err := conn.ExecContext(l.ctx, `INSERT INTO g4_commit_control (id, note) VALUES ($1, 'g4 control')`, i); err != nil {
			t.Fatalf("control insert %d: %v", i, err)
		}
		timings = append(timings, time.Since(start))
	}
	return timings
}

// walDeltaSummary renders the movement of the cluster's WAL and checkpointer
// counters between two readings, in a stable key order. lsn_bytes is exact.
// The wal_* counters lag: a backend publishes them when it goes idle, at most
// once a second, and walFlushSettled does not wait for a window under 64 KiB,
// so a window this small can read wal_bytes=0 beside a non-zero lsn_bytes. Read
// lsn_bytes for the volume and the ckpt_* counters for a checkpoint.
func walDeltaSummary(before, after walStats) string {
	delta := after.since(before)
	keys := make([]string, 0, len(delta))
	for k := range delta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, delta[k]))
	}
	return strings.Join(parts, " ")
}

// timedG4Link runs the gated link once, with the diagnostics around it that
// let a slow hosted-runner sample name its cause (#9275): the Postgres
// settings, the durable-commit control taken immediately before, the WAL and
// checkpoint movement across both, and the host's CPU model, load and
// pressure read right after. Everything but link is outside the timed call.
// The WAL counters are cluster-wide and flush on a throttle, so they are
// settled before the second reading; that wait is after the link, not in it.
func (l *ledgerDB) timedG4Link(t *testing.T, link func() linksfreshnessstore.LinkResult) (linksfreshnessstore.LinkResult, g4Report) {
	t.Helper()
	report := g4Report{samples: 1, settings: l.postgresSettings(t)}
	report.control = l.durableCommitControl(t)
	walBefore := readWALStats(t, l.raw)
	result := link()
	report.host = hostSnapshot()
	report.walDelta = walDeltaSummary(walBefore, readWALStatsSettled(t, l.raw, walBefore))
	report.total, report.phases = result.Duration, result.Phases
	return result, report
}
