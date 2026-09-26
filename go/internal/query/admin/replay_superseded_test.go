// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"net/http"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestReplayExplicitIDsOnSupersededGenerationRefusedEvenWithForce is the #7130
// admin-replay fence. The store fences superseded-generation projector rows
// out of every replay, so an explicit request naming one would otherwise
// answer 200 with replayed_count 0. It is refused by name instead, and force
// does not apply: replaying the row would re-project a retired generation.
func TestReplayExplicitIDsOnSupersededGenerationRefusedEvenWithForce(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	store := &stubAdminStore{
		claim:    ReplayIdempotencyClaim{Claimed: true},
		replayed: []WorkItem{{WorkItemID: "wi-live"}},
		supersededTargets: []SupersededReplayTarget{
			{WorkItemID: "wi-old-b", GenerationID: "gen-old"},
			{WorkItemID: "wi-old-a", GenerationID: "gen-old"},
		},
	}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	h := &Handler{Store: store, Audit: audit, Instruments: instruments}
	rec := postReplay(t, h, map[string]any{
		"work_item_ids":   []string{"wi-live", "wi-old-a", "wi-old-b"},
		"stage":           "projector",
		"reason":          "cause fixed",
		"idempotency_key": "k-superseded",
		"force":           true,
	}, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	if got["status"] != "refused" || got["reason"] == "" || got["detail"] == "" {
		t.Fatalf("want actionable refusal, got %+v", got)
	}
	refused, ok := got["refused_work_items"].([]any)
	if !ok || len(refused) != 2 {
		t.Fatalf("refused_work_items = %+v, want the 2 superseded ids", got["refused_work_items"])
	}
	first := refused[0].(map[string]any)
	if first["work_item_id"] != "wi-old-a" || first["generation_id"] != "gen-old" ||
		first["failure_class"] != failure.ReplayGenerationSupersededClass || first["reason"] == "" {
		t.Fatalf("refused_work_items[0] = %+v, want sorted wi-old-a on gen-old with its class and reason", first)
	}
	if store.supersededCalls != 1 || store.supersededFilter.Stage != "projector" {
		t.Fatalf("superseded read calls=%d filter=%+v, want one read scoped like the replay",
			store.supersededCalls, store.supersededFilter)
	}
	if store.claimCalls != 0 || store.replayFilter.WorkItemIDs != nil {
		t.Fatalf("refusal must not claim the key or replay (claims=%d filter=%+v)", store.claimCalls, store.replayFilter)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "replay_refused_superseded_generation" {
		t.Fatalf("want superseded-generation denied audit, got %+v", audit.Events)
	}
	assertAuditValid(t, audit.Events)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if got := supersededFenceCount(rm); got != 2 {
		t.Fatalf("eshu_dp_superseded_generation_fence_total = %d, want 2", got)
	}
}

// TestReplayBroadSelectorSkipsSupersededRead pins the read to explicit ids: a
// broad selector relies on the store-side fence and costs no extra query.
func TestReplayBroadSelectorSkipsSupersededRead(t *testing.T) {
	store := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}
	h := &Handler{Store: store, Audit: &testutil.FakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"stage":           "projector",
		"reason":          "retry transient failures",
		"idempotency_key": "k-broad",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if store.supersededCalls != 0 {
		t.Fatalf("superseded read calls = %d, want 0 for a broad selector", store.supersededCalls)
	}
}

func supersededFenceCount(rm metricdata.ResourceMetrics) int64 {
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_superseded_generation_fence_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if class, _ := point.Attributes.Value("failure_class"); class.AsString() == failure.ReplayGenerationSupersededClass {
					total += point.Value
				}
			}
		}
	}
	return total
}
