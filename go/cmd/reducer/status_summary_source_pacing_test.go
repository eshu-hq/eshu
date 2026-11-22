// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/workflow/snapshot"
)

// pacingScriptRows scans the two source-pacing statements' row shapes and
// reports no rows for every other coordinator statement.
type pacingScriptRows struct {
	rows  [][]any
	index int
}

func (r *pacingScriptRows) Next() bool   { return r.index < len(r.rows) }
func (r *pacingScriptRows) Err() error   { return nil }
func (r *pacingScriptRows) Close() error { return nil }
func (r *pacingScriptRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	r.index++
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i, target := range dest {
		switch t := target.(type) {
		case *string:
			*t = row[i].(string)
		case *bool:
			*t = row[i].(bool)
		case *int64:
			*t = row[i].(int64)
		case *float64:
			*t = row[i].(float64)
		case *sql.NullTime:
			*t = sql.NullTime{Time: row[i].(time.Time), Valid: true}
		default:
			return fmt.Errorf("unsupported scan target %T", target)
		}
	}
	return nil
}

type pacingScriptQueryer struct{ now time.Time }

func (q pacingScriptQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "FROM workflow_claim_pacing"):
		return &pacingScriptRows{rows: [][]any{{"vulnerability_intelligence", true, q.now.Add(-2 * time.Hour)}}}, nil
	case strings.Contains(query, "rows_past_engage_age"):
		return &pacingScriptRows{rows: [][]any{{"vulnerability_intelligence", float64(7200), int64(400), int64(400), int64(400)}}}, nil
	}
	return &pacingScriptRows{}, nil
}

func summaryEnabledEnv(extra map[string]string) func(string) string {
	env := map[string]string{statusSummaryWriterEnabledEnv: "true"}
	for key, value := range extra {
		env[key] = value
	}
	return envMap(env)
}

// TestStatusSummaryWriterStoresStuckFromItsOwnEnv proves #10882 F7: the
// coordinator companion computes with the writer process's
// ESHU_SOURCE_BACKPRESSURE_* thresholds and stores the stuck verdict, so a
// reader without the env still serves it.
func TestStatusSummaryWriterStoresStuckFromItsOwnEnv(t *testing.T) {
	configured := summaryEnabledEnv(map[string]string{
		"ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE":     "1h",
		"ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE":    "15m",
		"ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR":    "25",
		"ESHU_SOURCE_BACKPRESSURE_GRANT_DELAY":    "2s",
		"ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT": "30m",
	})
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		env       func(string) string
		wantStuck bool
	}{
		{"thresholds set", configured, true},
		{"thresholds unset", summaryEnabledEnv(nil), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner, err := statusSummaryWriterFor(tc.env, refusingDatabase{t}, nil, nil, nil)
			if err != nil || runner == nil {
				t.Fatalf("statusSummaryWriterFor() = %v, %v; want a runner", runner, err)
			}
			entries, err := runner.Companions[1].Compute(context.Background(), pacingScriptQueryer{now: now}, now)
			if err != nil {
				t.Fatalf("coordinator Compute() error = %v", err)
			}
			decoded, err := snapshot.Decode(entries)
			if err != nil || decoded == nil || len(decoded.SourcePacing) != 1 {
				t.Fatalf("stored snapshot = %+v, %v; want one source_pacing row", decoded, err)
			}
			if got := decoded.SourcePacing[0].Stuck; got != tc.wantStuck {
				t.Fatalf("stored stuck = %v, want %v", got, tc.wantStuck)
			}
		})
	}
}

// TestStatusSummaryWriterRefusesPartialBackpressureThresholds keeps the
// writer on the same contract as the live reader: a half-set threshold list
// is a startup error, never silent unstuck-by-default rows.
func TestStatusSummaryWriterRefusesPartialBackpressureThresholds(t *testing.T) {
	partial := summaryEnabledEnv(map[string]string{"ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE": "1h"})
	if _, err := statusSummaryWriterFor(partial, refusingDatabase{t}, nil, nil, nil); err == nil {
		t.Fatal("statusSummaryWriterFor() error = nil, want a startup error for partial thresholds")
	}
}
