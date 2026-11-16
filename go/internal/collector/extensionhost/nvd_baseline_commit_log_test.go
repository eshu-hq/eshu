// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBaselineCommitLogsNotCoveredReason pins the operator signal for the
// not_covered outcome: one WARN per decided generation carrying a bounded
// reason, so an operator can tell the four causes behind the counter apart
// without reading the payload. The reason set is closed; no payload text
// (the sha, the stamps) rides the log.
func TestBaselineCommitLogsNotCoveredReason(t *testing.T) {
	t.Parallel()

	const chunkStart = int64(1009843200000000)
	cases := []struct {
		name       string
		mutate     func(map[string]any)
		wantReason string
	}{
		{
			name: "advance missing", wantReason: "advance_missing",
			mutate: func(p map[string]any) { delete(p, "nvd_baseline_advance_to_unix_us") },
		},
		{
			name: "advance non-positive", wantReason: "advance_missing",
			mutate: func(p map[string]any) { p["nvd_baseline_advance_to_unix_us"] = int64(0) },
		},
		{
			name: "blank sha", wantReason: "sha_malformed",
			mutate: func(p map[string]any) { p["nvd_baseline_year_sha256"] = "" },
		},
		{
			name: "malformed sha", wantReason: "sha_malformed",
			mutate: func(p map[string]any) { p["nvd_baseline_year_sha256"] = "not-a-sha" },
		},
		{
			// First match wins: sha_malformed is checked before the
			// advance-behind-start rule, so a combined defect logs the sha.
			name: "malformed sha and advance behind start", wantReason: "sha_malformed",
			mutate: func(p map[string]any) {
				p["nvd_baseline_year_sha256"] = "not-a-sha"
				p["nvd_baseline_advance_to_unix_us"] = chunkStart - 1000
			},
		},
		{
			name: "advance behind start", wantReason: "advance_behind_start",
			mutate: func(p map[string]any) { p["nvd_baseline_advance_to_unix_us"] = chunkStart - 1000 },
		},
		{
			name: "complete advance behind start", wantReason: "advance_behind_start",
			mutate: func(p map[string]any) {
				p["nvd_baseline_advance_to_unix_us"] = chunkStart - 1000
				p["nvd_baseline_year_complete"] = true
			},
		},
		{
			name: "incomplete advance at start", wantReason: "advance_not_past_start",
			mutate: func(p map[string]any) { p["nvd_baseline_advance_to_unix_us"] = chunkStart },
		},
		{
			name: "sub-millisecond advance floors to start", wantReason: "advance_not_past_start",
			mutate: func(p map[string]any) { p["nvd_baseline_advance_to_unix_us"] = chunkStart + 500 },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := testNVDBaselineVerdictPayload()
			tc.mutate(payload)
			records := decideBaselineWithLogs(t, payload)
			if len(records) != 1 {
				t.Fatalf("log records = %v, want exactly one not-covered WARN", records)
			}
			record := records[0]
			if record["level"] != "WARN" {
				t.Fatalf("log level = %v, want WARN", record["level"])
			}
			if record["reason"] != tc.wantReason {
				t.Fatalf("reason = %v, want %q", record["reason"], tc.wantReason)
			}
			if record["msg"] != "nvd baseline generation not covered by commit policy, checkpoint holds" {
				t.Fatalf("msg = %v, want the not-covered message", record["msg"])
			}
			if record["work_item_id"] != "work-scorecard-1" {
				t.Fatalf("work_item_id = %v, want work-scorecard-1", record["work_item_id"])
			}
			if record["generation_id"] != "generation-1" {
				t.Fatalf("generation_id = %v, want generation-1", record["generation_id"])
			}
			if record["baseline_run_id"] != "baseline-2026-11" {
				t.Fatalf("baseline_run_id = %v, want baseline-2026-11", record["baseline_run_id"])
			}
			if record["collector_instance_id"] != "scorecard-instance" {
				t.Fatalf("collector_instance_id = %v, want scorecard-instance", record["collector_instance_id"])
			}
			if _, leaked := record["year_sha256"]; leaked {
				t.Fatalf("log record carries the sha: %v", record)
			}
		})
	}
}

// TestBaselineCommitLogsNothingOnAdvanceOrHold proves the not-covered WARN
// is specific: an advancing verdict, a complete verdict at the chunk start,
// and a hold verdict log nothing.
func TestBaselineCommitLogsNothingOnAdvanceOrHold(t *testing.T) {
	t.Parallel()

	cases := map[string]func(map[string]any){
		"advanced": func(map[string]any) {},
		"complete advance at start": func(p map[string]any) {
			p["nvd_baseline_advance_to_unix_us"] = int64(1009843200000000)
			p["nvd_baseline_year_complete"] = true
		},
		"held": func(p map[string]any) { p["nvd_baseline_should_advance"] = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			payload := testNVDBaselineVerdictPayload()
			mutate(payload)
			if records := decideBaselineWithLogs(t, payload); len(records) != 0 {
				t.Fatalf("log records = %v, want none", records)
			}
		})
	}
}

func decideBaselineWithLogs(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	policy, err := NewNVDBaselineCommitPolicy("scorecard-instance", nil, logger)
	if err != nil {
		t.Fatalf("NewNVDBaselineCommitPolicy() error = %v, want nil", err)
	}
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	if _, err := policy.CommitPolicy(item, time.Now().UTC(), []facts.Envelope{testNVDWindowSnapshotEnvelope(payload)}); err != nil {
		t.Fatalf("CommitPolicy() error = %v, want a hold or advance", err)
	}
	return logRecords(t, logs)
}
