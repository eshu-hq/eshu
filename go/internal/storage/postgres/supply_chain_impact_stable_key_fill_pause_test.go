// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestStableKeyFillPauseReasons pins each pause condition of ruling 1.6 and that an
// unreadable condition is never clear.
func TestStableKeyFillPauseReasons(t *testing.T) {
	t.Parallel()

	limits := StableKeyFillLimits{MaxReplayLag: 60 * time.Second, MinFreeDiskBytes: 100e9}
	clear := StableKeyFillPauseState{ReplayLag: 5 * time.Second, FreeDiskBytes: 500e9, DiskKnown: true}
	for name, tc := range map[string]struct {
		limits StableKeyFillLimits
		state  StableKeyFillPauseState
		want   string
	}{
		"clear":                        {limits, clear, ""},
		"replay lag at the limit":      {limits, StableKeyFillPauseState{ReplayLag: 60 * time.Second, FreeDiskBytes: 500e9, DiskKnown: true}, ""},
		"replay lag over":              {limits, StableKeyFillPauseState{ReplayLag: 61 * time.Second, FreeDiskBytes: 500e9, DiskKnown: true}, telemetry.SupplyChainImpactStableKeyFillPauseReplayLag},
		"free disk under":              {limits, StableKeyFillPauseState{FreeDiskBytes: 99e9, DiskKnown: true}, telemetry.SupplyChainImpactStableKeyFillPauseFreeDisk},
		"free disk at the limit":       {limits, StableKeyFillPauseState{FreeDiskBytes: 100e9, DiskKnown: true}, ""},
		"disk unreadable":              {limits, StableKeyFillPauseState{}, telemetry.SupplyChainImpactStableKeyFillPauseDiskUnreadable},
		"index build":                  {limits, StableKeyFillPauseState{FreeDiskBytes: 500e9, DiskKnown: true, IndexBuilding: true}, telemetry.SupplyChainImpactStableKeyFillPauseIndexBuild},
		"disk limit off":               {StableKeyFillLimits{MaxReplayLag: 60 * time.Second}, StableKeyFillPauseState{}, ""},
		"lag limit off":                {StableKeyFillLimits{MinFreeDiskBytes: 100e9}, StableKeyFillPauseState{ReplayLag: time.Hour, FreeDiskBytes: 500e9, DiskKnown: true}, ""},
		"index build beats limits off": {StableKeyFillLimits{}, StableKeyFillPauseState{IndexBuilding: true}, telemetry.SupplyChainImpactStableKeyFillPauseIndexBuild},
	} {
		if got := tc.limits.PauseReason(tc.state); got != tc.want {
			t.Errorf("%s: PauseReason = %q, want %q", name, got, tc.want)
		}
	}
}

func TestStableKeyFillProbeReadsLagIndexBuildAndDisk(t *testing.T) {
	t.Parallel()

	q := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{float64(12.5)}}},
		{rows: [][]any{{true}}},
	}}
	probe := StableKeyFillProbe{DB: q, FreeDisk: func(context.Context) (int64, bool, error) { return 250e9, true, nil }}
	state, err := probe.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if state.ReplayLag != 12500*time.Millisecond || !state.IndexBuilding || state.FreeDiskBytes != 250e9 || !state.DiskKnown {
		t.Fatalf("state = %+v", state)
	}
	if len(q.queries) != 2 ||
		!strings.Contains(q.queries[0], "pg_stat_replication") || !strings.Contains(q.queries[0], "replay_lag") ||
		!strings.Contains(q.queries[1], "pg_stat_progress_create_index") {
		t.Fatalf("probe statements = %v", q.queries)
	}
	for _, query := range q.queries {
		if strings.Contains(query, "pg_current_wal_lsn") {
			t.Fatalf("the probe reads the log position, which every writer moves: %s", query)
		}
	}

	// A condition that cannot be read is an error, never a clear reading.
	failing := StableKeyFillProbe{DB: &fakeQueryer{responses: []fakeRows{{err: errors.New("permission denied")}}}}
	if _, err := failing.Read(context.Background()); err == nil {
		t.Fatal("an unreadable replay lag was taken as clear")
	}
	noDisk := StableKeyFillProbe{DB: &fakeQueryer{responses: []fakeRows{{rows: [][]any{{float64(0)}}}, {rows: [][]any{{false}}}}}}
	state, err = noDisk.Read(context.Background())
	if err != nil || state.DiskKnown {
		t.Fatalf("no disk reading wired: state=%+v err=%v, want DiskKnown false", state, err)
	}
}

// TestStableKeyFillPaceKeepsTheEstimateInsideTheBudget: 500 rows at 1 KiB each at
// 8 GB an hour is a 0.23 s floor between chunks, so a full day of chunks stays at
// or under the budget; a zero budget never runs, and an empty step waits for nothing.
func TestStableKeyFillPaceKeepsTheEstimateInsideTheBudget(t *testing.T) {
	t.Parallel()

	pace := StableKeyFillPace{BytesPerHour: 8_000_000_000, BytesPerRow: 1024}
	delay := pace.Delay(500)
	if delay < 229*time.Millisecond || delay > 232*time.Millisecond {
		t.Fatalf("Delay(500) = %v, want about 230ms for 500 KiB at 8 GB/h", delay)
	}
	if got := float64(500*1024) / delay.Hours(); got > 8_000_000_000*1.001 {
		t.Fatalf("a chunk every %v writes %.0f bytes an hour, over the 8e9 budget", delay, got)
	}
	if pace.Delay(0) != 0 {
		t.Fatal("an empty step waits")
	}
	if (StableKeyFillPace{BytesPerRow: 1024}).Delay(1) < time.Hour {
		t.Fatal("a fill with no WAL budget does not wait out the hour")
	}
	if pace.Delay(1000) <= pace.Delay(500) {
		t.Fatal("a larger step does not wait longer")
	}
}
