// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8472 pacing depth signal proof on real PostgreSQL: the per-domain pending-depth gauge an operator reads to answer why pacing engaged is recorded from the same depth observation that feeds the pacing verdict
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// attachPacingSignalInstruments wires an in-memory meter into the store and
// returns its reader, so pacing proofs assert on the operator signals
// themselves instead of inferring them from verdicts.
func attachPacingSignalInstruments(t *testing.T, store *WorkflowControlStore) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	inst, err := telemetry.NewInstruments(provider.Meter("pacing-signals-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v, want nil", err)
	}
	store.Instruments = inst
	return reader
}

// pacingStreamAttrsEqual reports whether a datapoint's attribute set is
// exactly the wanted set: no missing label and no extra label.
func pacingStreamAttrsEqual(set attribute.Set, want map[string]string) bool {
	got := set.ToSlice()
	if len(got) != len(want) {
		return false
	}
	for _, kv := range got {
		w, ok := want[string(kv.Key)]
		if !ok || kv.Value.AsString() != w {
			return false
		}
	}
	return true
}

// pacingGaugeValue returns the latest value of a synchronous gauge stream
// with exactly the wanted attribute set. It reports false when the stream
// is absent, so a missing signal reads as absent rather than zero.
func pacingGaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string, wantAttrs map[string]string) (int64, bool) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("metric %q data is %T, want Gauge[int64]", name, m.Data)
			}
			for _, dp := range gauge.DataPoints {
				if pacingStreamAttrsEqual(dp.Attributes, wantAttrs) {
					return dp.Value, true
				}
			}
		}
	}
	return 0, false
}

// TestWorkflowClaimPacingRecordsDepthPendingGauge proves the per-domain
// depth observer is operator-visible: the same depth observation that feeds
// the pacing verdict is recorded on the pending-depth gauge, once for the
// claiming family (with the domain label) and once system-wide (without
// it), so an operator can see why pacing engaged from metrics alone.
func TestWorkflowClaimPacingRecordsDepthPendingGauge(t *testing.T) {
	_, store, ctx, _ := openClaimDepthHarness(t, 1)
	reader := attachPacingSignalInstruments(t, store)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seedClaimDepthBacklog(t, store, ctx, "pacing-depth-gauge", base,
		[]claimDepthKeyBacklog{{FairnessKey: "pacing:hot", Count: 60}})
	cfg := testClaimPacingConfig()
	selector := workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "pacing-owner",
		ClaimID:             "pacing-claim-1",
	}

	decision, err := store.CheckClaimPacing(ctx, selector, cfg, time.Now().UTC())
	if err != nil {
		t.Fatalf("CheckClaimPacing() error = %v, want nil", err)
	}
	if !decision.Paced {
		t.Fatal("CheckClaimPacing() Paced = false, want true with 60 pending over domain high 50")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	domain := fmt.Sprintf("%s/%s", scope.CollectorGit, "claim-depth-instance")
	if got, ok := pacingGaugeValue(t, rm, "eshu_dp_workflow_claim_pacing_depth_pending",
		map[string]string{"domain": domain}); !ok || got != 60 {
		t.Fatalf("depth_pending{domain=%q} = %d, present=%v; want 60, present", domain, got, ok)
	}
	if got, ok := pacingGaugeValue(t, rm, "eshu_dp_workflow_claim_pacing_depth_pending",
		map[string]string{}); !ok || got != 60 {
		t.Fatalf("depth_pending{} (global) = %d, present=%v; want 60, present", got, ok)
	}
}
