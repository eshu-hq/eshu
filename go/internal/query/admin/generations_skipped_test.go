// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// skippedResult builds a refinalize result that skipped scopes for two reasons.
func skippedResult() recovery.RefinalizeResult {
	var skipped recovery.SkippedScopes
	skipped.Add(recovery.SkipReasonNoRecoverableGeneration, "repository:r_874801ea")
	skipped.Add(recovery.SkipReasonNoRecoverableGeneration, "repository:r_other")
	skipped.Add(recovery.SkipReasonUnknownScope, "repository:r_missing")
	return recovery.RefinalizeResult{
		Enqueued: 2,
		ScopeIDs: []string{"scope-1", "scope-2"},
		Skipped:  skipped,
	}
}

// decodeSkippedScopes pulls the skipped_scopes object out of a response body.
func decodeSkippedScopes(t *testing.T, body map[string]any) map[string]any {
	t.Helper()

	skipped, ok := body["skipped_scopes"].(map[string]any)
	if !ok {
		t.Fatalf("response has no skipped_scopes object: %v", body)
	}
	return skipped
}

// TestAdminHandler_RecoverGenerations_ReportsSkippedScopes is the #7116
// visibility contract: a rebuild that left scopes out must say so, by reason,
// or an operator reads "enqueued: 2" as a complete rebuild.
func TestAdminHandler_RecoverGenerations_ReportsSkippedScopes(t *testing.T) {
	recoveryStub := &stubRecoveryHandler{refinalizeResult: skippedResult()}
	h := &Handler{Recovery: recoveryStub, Store: &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/recover-generations", map[string]any{
		"all_scopes":      true,
		"reason":          "graph rebuild after backend swap",
		"idempotency_key": "skip-report-1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	skipped := decodeSkippedScopes(t, decodeBody(t, w))
	if got := int(skipped["total"].(float64)); got != 3 {
		t.Fatalf("skipped_scopes.total = %d, want 3", got)
	}
	byReason := skipped["by_reason"].(map[string]any)
	if got := int(byReason[recovery.SkipReasonNoRecoverableGeneration].(float64)); got != 2 {
		t.Fatalf("by_reason[no_recoverable_generation] = %d, want 2", got)
	}
	if got := int(byReason[recovery.SkipReasonUnknownScope].(float64)); got != 1 {
		t.Fatalf("by_reason[unknown_scope] = %d, want 1", got)
	}
	samples := skipped["sample_scope_ids"].(map[string]any)
	first := samples[recovery.SkipReasonNoRecoverableGeneration].([]any)
	if len(first) != 2 || first[0] != "repository:r_874801ea" {
		t.Fatalf("sample_scope_ids[no_recoverable_generation] = %v, want the two skipped scope ids", first)
	}
}

// TestAdminHandler_RecoverGenerations_ReportsEmptySkippedScopes pins that the
// report is always present. A response that only carries skipped_scopes when
// something was skipped makes "nothing skipped" indistinguishable from "this
// server predates the report".
func TestAdminHandler_RecoverGenerations_ReportsEmptySkippedScopes(t *testing.T) {
	recoveryStub := &stubRecoveryHandler{refinalizeResult: recovery.RefinalizeResult{Enqueued: 1, ScopeIDs: []string{"scope-1"}}}
	h := &Handler{Recovery: recoveryStub, Store: &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/recover-generations", map[string]any{
		"scope_ids":       []string{"scope-1"},
		"reason":          "wedged",
		"idempotency_key": "skip-report-empty",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	skipped := decodeSkippedScopes(t, decodeBody(t, w))
	if got := int(skipped["total"].(float64)); got != 0 {
		t.Fatalf("skipped_scopes.total = %d, want 0", got)
	}
	if byReason, ok := skipped["by_reason"].(map[string]any); !ok || len(byReason) != 0 {
		t.Fatalf("skipped_scopes.by_reason = %v, want an empty object", skipped["by_reason"])
	}
}

// TestAdminHandler_Refinalize_ReportsSkippedScopes gives the legacy refinalize
// route the same report, since it runs the same selection.
func TestAdminHandler_Refinalize_ReportsSkippedScopes(t *testing.T) {
	h := &Handler{Recovery: &stubRecoveryHandler{refinalizeResult: skippedResult()}}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/refinalize", map[string]any{"scope_ids": []string{"scope-1"}})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	skipped := decodeSkippedScopes(t, decodeBody(t, w))
	if got := int(skipped["total"].(float64)); got != 3 {
		t.Fatalf("skipped_scopes.total = %d, want 3", got)
	}
}

// TestAdminHandler_RecoverGenerations_EmitsSkippedScopeSignals proves the 3 AM
// path: the same counts land in a structured log line and in a per-reason
// counter, so an operator with only logs and dashboards can see a partial
// rebuild without holding the HTTP response.
func TestAdminHandler_RecoverGenerations_EmitsSkippedScopeSignals(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}

	h := &Handler{
		Recovery:    &stubRecoveryHandler{refinalizeResult: skippedResult()},
		Store:       &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}},
		Instruments: instruments,
	}
	mux := newAdminMux(h)
	w := postJSON(mux, "/api/v0/admin/recover-generations", map[string]any{
		"all_scopes":      true,
		"reason":          "graph rebuild after backend swap",
		"idempotency_key": "skip-signals-1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &line); err != nil {
		t.Fatalf("log output is not one JSON line: %v\n%s", err, logs.String())
	}
	if line["msg"] != "recover-generations completed" {
		t.Fatalf("log msg = %v, want recover-generations completed", line["msg"])
	}
	if got := int(line["skipped_total"].(float64)); got != 3 {
		t.Fatalf("log skipped_total = %d, want 3", got)
	}
	if got := int(line["enqueued"].(float64)); got != 2 {
		t.Fatalf("log enqueued = %d, want 2", got)
	}
	if got := int(line["skipped_"+recovery.SkipReasonNoRecoverableGeneration].(float64)); got != 2 {
		t.Fatalf("log skipped_no_recoverable_generation = %d, want 2", got)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "eshu_dp_recovery_scopes_skipped_total" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", metric.Data)
			}
			for _, point := range sum.DataPoints {
				reason, _ := point.Attributes.Value("reason")
				got[reason.AsString()] += point.Value
			}
		}
	}
	if got[recovery.SkipReasonNoRecoverableGeneration] != 2 || got[recovery.SkipReasonUnknownScope] != 1 || len(got) != 2 {
		t.Fatalf("eshu_dp_recovery_scopes_skipped_total by reason = %v, want no_recoverable_generation=2 unknown_scope=1", got)
	}
}

// TestAdminHandler_RecoverGenerations_AuditDistinguishesPartialRebuild is the
// #7116 durable-record contract. The governance audit ledger is what survives a
// log rotation, so a rebuild that left scopes out must not be recorded under
// the same reason code as a complete one. Event carries no free-form field, so
// the reason code is the only place the partial outcome can live.
func TestAdminHandler_RecoverGenerations_AuditDistinguishesPartialRebuild(t *testing.T) {
	tests := []struct {
		name       string
		result     recovery.RefinalizeResult
		wantReason string
	}{
		{
			name:       "skipped scopes are recorded as partial",
			result:     skippedResult(),
			wantReason: "recover_generations_accepted_partial",
		},
		{
			name: "nothing skipped keeps the complete reason",
			result: recovery.RefinalizeResult{
				Enqueued: 1,
				ScopeIDs: []string{"scope-1"},
			},
			wantReason: "recover_generations_accepted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audit := &testutil.FakeGovernanceAuditAppender{}
			h := &Handler{
				Recovery: &stubRecoveryHandler{refinalizeResult: tt.result},
				Store:    &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}},
				Audit:    audit,
			}
			w := postJSON(newAdminMux(h), "/api/v0/admin/recover-generations", map[string]any{
				"all_scopes":      true,
				"reason":          "graph rebuild after backend swap",
				"idempotency_key": "audit-partial-" + tt.wantReason,
			})
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
			}
			if len(audit.Events) != 1 {
				t.Fatalf("audit events = %d, want 1: %+v", len(audit.Events), audit.Events)
			}
			if got := audit.Events[0].ReasonCode; got != tt.wantReason {
				t.Fatalf("audit reason code = %q, want %q", got, tt.wantReason)
			}
			if got := audit.Events[0].Decision; got != governanceaudit.DecisionAllowed {
				t.Fatalf("audit decision = %q, want allowed: a partial rebuild is still an accepted one", got)
			}
			assertAuditValid(t, audit.Events)
		})
	}
}
