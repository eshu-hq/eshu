// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// deltaReportFixture builds n delta-active pairs; every third one is a
// reindex_unsupported scope, the rest reindex_requested.
func deltaReportFixture(n int) (recovery.DeltaActiveScopes, []recovery.DeltaActiveGeneration) {
	var report recovery.DeltaActiveScopes
	delta := make([]recovery.DeltaActiveGeneration, 0, n)
	for i := range n {
		outcome := recovery.DeltaActiveOutcomeReindexRequested
		if i%3 == 2 {
			outcome = recovery.DeltaActiveOutcomeReindexUnsupported
		}
		scopeID := fmt.Sprintf("git-repository-scope:repo-%03d", i)
		report.Add(outcome, scopeID)
		delta = append(delta, recovery.DeltaActiveGeneration{
			ScopeID:      scopeID,
			GenerationID: fmt.Sprintf("gen-%03d", i),
			Outcome:      outcome,
		})
	}
	return report, delta
}

// deltaReportLog is one decoded JSON log record.
type deltaReportLog map[string]any

func decodeDeltaReportLogs(t *testing.T, buf *bytes.Buffer) []deltaReportLog {
	t.Helper()
	var records []deltaReportLog
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record deltaReportLog
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

// TestReportDeltaActiveCapsPerScopeWarnings pins the #7797 log budget: the
// first recovery.DeltaActiveScopeSampleLimit delta-active scopes log at WARN,
// every further scope logs the same fields at INFO, and one summary WARN
// carries the exact counts per outcome. A recovery of 161 delta-active scopes
// therefore emits 11 WARN lines, not 161.
func TestReportDeltaActiveCapsPerScopeWarnings(t *testing.T) {
	t.Parallel()

	limit := recovery.DeltaActiveScopeSampleLimit
	for _, n := range []int{0, 1, limit, limit + 1, 161} {
		t.Run(fmt.Sprintf("%d_scopes", n), func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			store := NewRecoveryStore(nil, WithRecoveryLogger(logger))
			report, delta := deltaReportFixture(n)
			store.reportDeltaActive(context.Background(), report, delta)

			var scopeWarn, scopeInfo, summary int
			var summaryRecord deltaReportLog
			for _, record := range decodeDeltaReportLogs(t, &buf) {
				_, perScope := record["scope_id"]
				switch {
				case perScope && record["level"] == "WARN":
					scopeWarn++
				case perScope && record["level"] == "INFO":
					scopeInfo++
				case !perScope && record["level"] == "WARN":
					summary++
					summaryRecord = record
				default:
					t.Fatalf("unexpected log record %v", record)
				}
				if perScope && (record["generation_id"] == nil || record["outcome"] == nil) {
					t.Fatalf("per-scope record %v lacks generation_id or outcome", record)
				}
			}

			wantWarn := min(n, limit)
			wantInfo := max(n-limit, 0)
			wantSummary := 0
			if n > 0 {
				wantSummary = 1
			}
			if scopeWarn != wantWarn || scopeInfo != wantInfo || summary != wantSummary {
				t.Fatalf("n=%d: per-scope WARN=%d INFO=%d summary WARN=%d, want %d/%d/%d",
					n, scopeWarn, scopeInfo, summary, wantWarn, wantInfo, wantSummary)
			}
			if n == 0 {
				return
			}
			for key, want := range map[string]int{
				"delta_active_total":                          n,
				recovery.DeltaActiveOutcomeReindexRequested:   report.ByOutcome[recovery.DeltaActiveOutcomeReindexRequested],
				recovery.DeltaActiveOutcomeReindexUnsupported: report.ByOutcome[recovery.DeltaActiveOutcomeReindexUnsupported],
				"per_scope_warn_limit":                        limit,
				"per_scope_info_count":                        wantInfo,
			} {
				got, ok := summaryRecord[key].(float64)
				if !ok || int(got) != want {
					t.Fatalf("n=%d: summary %s = %v, want %d (record %v)", n, key, summaryRecord[key], want, summaryRecord)
				}
			}
			if msg, _ := summaryRecord["msg"].(string); !strings.Contains(msg, "see the response's delta_active_scopes for samples") {
				t.Fatalf("summary message %q does not point to the response samples", msg)
			}
		})
	}
}
