// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestReadinessUnavailableLogsBoundedCause is the #9227 F4 proof: each
// deterministic fail-closed readiness cause (malformed or duplicate configured
// source targets, more than 200 relevant targets, unready consumption keys)
// and a transient query error leave one WARN readiness_snapshot completion
// line naming a closed-set cause, on both the findings and explain routes.
func TestReadinessUnavailableLogsBoundedCause(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		err   error
		cause string
	}{
		{"malformed targets", fmt.Errorf("read: %w", impact.ErrReadinessSourceTargetsMalformed), "source_targets_malformed"},
		{"target overflow", impact.ErrReadinessSourceTargetsOverflow, "source_targets_overflow"},
		{"consumption keys", impact.ErrReadinessConsumptionKeysNotReady, "consumption_keys_not_ready"},
		{"query error", errors.New("read supply chain impact readiness: connection reset"), "query_error"},
	} {
		for _, route := range []string{
			failedStageRoute,
			"/api/v0/supply-chain/impact/explain?finding_id=finding-environment-evidence-parity",
		} {
			t.Run(tc.name+" "+route[:len("/api/v0/supply-chain/impact/explain")], func(t *testing.T) {
				t.Parallel()
				var logBuf bytes.Buffer
				row := failedStageRow()
				handler := &Handler{
					ImpactFindings: &graph.FakeRuntimeContextFindingStore{
						Rows: []impact.FindingRow{row}, ByRepo: map[string]impact.RuntimeContext{},
					},
					ImpactExplanations: &evidenceExplanationStore{row: impact.ExplanationRow{Finding: row}},
					Readiness:          &evidenceReadinessStore{err: tc.err},
					Logger:             slog.New(slog.NewJSONHandler(&logBuf, nil)),
				}
				mux := http.NewServeMux()
				handler.Mount(mux)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route, nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
				}
				var readinessLine map[string]any
				for _, record := range recordsWithEvent(decodeLogRecords(t, &logBuf), "supply_chain_query.stage_completed") {
					if record["stage"] == "readiness_snapshot" {
						readinessLine = record
					}
				}
				if readinessLine == nil {
					t.Fatal("no readiness_snapshot completion line")
				}
				if readinessLine["level"] != "WARN" || readinessLine["error"] != true ||
					readinessLine["readiness_unavailable_cause"] != tc.cause ||
					readinessLine["readiness_error"] != tc.err.Error() {
					t.Fatalf("readiness completion line = %#v, want WARN cause %s", readinessLine, tc.cause)
				}
			})
		}
	}
}
