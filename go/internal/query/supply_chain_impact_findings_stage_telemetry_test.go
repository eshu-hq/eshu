// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// TestSupplyChainListImpactFindingsEmitsPerStageTelemetry is the #7007
// regression proof for the route's missing stage-level signal: issue #7007
// reported 13-25s requests on ops-qa with "no repository_query.stage_*,
// no query.graph_read.*, nothing tied to the request" in the API pod logs.
// This locks in that every backing read the handler issues — the findings
// query itself, plus the readiness-snapshot Postgres read and the three
// graph probes — now emits a bounded supply_chain_query.stage_started /
// stage_completed pair an operator can attribute request latency to.
func TestSupplyChainListImpactFindingsEmitsPerStageTelemetry(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []impact.FindingRow{{FindingID: "finding-1", CVEID: "CVE-2026-0001"}},
	}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	handler := &SupplyChainHandler{ImpactFindings: store, Logger: logger}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	logged := logBuf.String()
	for _, stage := range []string{
		"impact_findings_query",
		"cloud_runtime_evidence",
		"kubernetes_runtime_evidence",
		"runtime_context",
		"readiness_snapshot",
	} {
		startedMarker := `"event_name":"supply_chain_query.stage_started"`
		completedMarker := `"event_name":"supply_chain_query.stage_completed"`
		stageMarker := `"stage":"` + stage + `"`
		if !containsLineWithBoth(logged, startedMarker, stageMarker) {
			t.Fatalf("missing supply_chain_query.stage_started for stage=%q; log=%s", stage, logged)
		}
		if !containsLineWithBoth(logged, completedMarker, stageMarker) {
			t.Fatalf("missing supply_chain_query.stage_completed for stage=%q; log=%s", stage, logged)
		}
	}
}

// containsLineWithBoth reports whether any single line of logged contains
// both substrings, so a "stage" value from one stage's event does not falsely
// satisfy a different stage's assertion.
func containsLineWithBoth(logged, a, b string) bool {
	for _, line := range strings.Split(logged, "\n") {
		if strings.Contains(line, a) && strings.Contains(line, b) {
			return true
		}
	}
	return false
}
