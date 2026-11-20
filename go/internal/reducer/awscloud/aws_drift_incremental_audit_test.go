// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// auditCorrectionsTotal collects the corrections counter from a test meter
// reader: the sum over the closed counter name, or -1 when the series is
// absent (a run that must emit nothing records no point).
func auditCorrectionsTotal(t *testing.T, reader sdkmetric.Reader) float64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_aws_drift_incremental_audit_corrections_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("corrections data = %T, want Sum[int64]", m.Data)
			}
			var total float64
			for _, p := range sum.DataPoints {
				total += float64(p.Value)
			}
			return total
		}
	}
	return -1
}

// deltaAuditFixtureRows returns two orphaned-admissible rows: arn:keep
// (unchanged across the audit) and arn:new (added by the audit).
func deltaAuditFixtureRows() []cloudruntime.AddressedRow {
	return []cloudruntime.AddressedRow{
		{ARN: "arn:keep", ResourceType: "aws_lambda_function", Cloud: &cloudruntime.ResourceRow{
			ARN: "arn:keep", ResourceID: "keep", ResourceType: "aws_lambda_function",
		}},
		{ARN: "arn:new", ResourceType: "aws_lambda_function", Cloud: &cloudruntime.ResourceRow{
			ARN: "arn:new", ResourceID: "new", ResourceType: "aws_lambda_function",
		}},
	}
}

// TestAWSCloudRuntimeDriftHandlerAuditCountsMissedUnchangedKeys pins the D6
// production wiring: an audit_due full run classifies every loaded row,
// reconstructs the delta that the same diff would have classified, and emits
// exactly the admitted keys the delta missed (here arn:keep, whose identity
// never changed, so a delta would never have reclassified it).
func TestAWSCloudRuntimeDriftHandlerAuditCountsMissedUnchangedKeys(t *testing.T) {
	t.Parallel()
	inst, reader := newAWSCloudRuntimeDriftInstruments(t)
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: map[string]string{"arn:keep": "p1"},
		NextSnapshot: map[string]string{"arn:keep": "p1", "arn:new": "p9"},
		LastAuditAt:  time.Now().Add(-25 * time.Hour),
		Now:          time.Now(),
	})
	if plan.Mode != AWSDriftIncrementalModeFull || plan.Reason != AWSDriftIncrementalReasonAuditDue {
		t.Fatalf("plan = %+v, want full/audit_due", plan)
	}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader: &stubAWSCloudRuntimeDriftEvidenceLoader{
			rows: deltaAuditFixtureRows(),
		},
		Writer:             &stubAWSCloudRuntimeDriftFindingWriter{},
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{7}},
		Instruments:        inst,
		IncrementalPlan:    &plan,
	}
	if _, err := handler.Handle(t.Context(), reducercontract.Intent{
		IntentID: "intent-audit", ScopeID: "aws:scope", GenerationID: "gen-1",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := auditCorrectionsTotal(t, reader); got != 1 {
		t.Fatalf("audit corrections = %v, want 1 (arn:keep missed by the delta reconstruction)", got)
	}
}

// TestAWSCloudRuntimeDriftHandlerDeltaRunEmitsNoCorrections pins the emit
// gate: delta and fallback-full runs never touch the corrections counter,
// and a zero-miss audit records no point.
func TestAWSCloudRuntimeDriftHandlerDeltaRunEmitsNoCorrections(t *testing.T) {
	t.Parallel()
	deltaPlan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: map[string]string{"arn:keep": "p1"},
		NextSnapshot: map[string]string{"arn:keep": "p1", "arn:new": "p9"},
		LastAuditAt:  time.Now().Add(-time.Hour),
		Now:          time.Now(),
	})
	if deltaPlan.Mode != AWSDriftIncrementalModeDelta {
		t.Fatalf("plan = %+v, want delta", deltaPlan)
	}
	for _, tc := range []struct {
		name string
		plan AWSDriftIncrementalPlan
	}{
		{"delta", deltaPlan},
		{"fallback", AWSDriftIncrementalPlan{Mode: AWSDriftIncrementalModeFull, Reason: AWSDriftIncrementalReasonFallbackForced}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst, reader := newAWSCloudRuntimeDriftInstruments(t)
			handler := AWSCloudRuntimeDriftHandler{
				EvidenceLoader: &stubAWSCloudRuntimeDriftEvidenceLoader{
					rows: deltaAuditFixtureRows(),
				},
				Writer:             &stubAWSCloudRuntimeDriftFindingWriter{},
				FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{7}},
				Instruments:        inst,
				IncrementalPlan:    &tc.plan,
			}
			if _, err := handler.Handle(t.Context(), reducercontract.Intent{
				IntentID: "intent-" + tc.name, ScopeID: "aws:scope", GenerationID: "gen-1",
				SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
			}); err != nil {
				t.Fatalf("Handle() error = %v, want nil", err)
			}
			if got := auditCorrectionsTotal(t, reader); got != -1 {
				t.Fatalf("audit corrections = %v, want absent (no point on %s runs)", got, tc.name)
			}
		})
	}
}

// TestAWSCloudRuntimeDriftHandlerZeroMissAuditEmitsNothing pins that an
// audit whose diff covers every admitted key emits no point: the
// reconstruction agrees with the full run, so there is nothing to count.
func TestAWSCloudRuntimeDriftHandlerZeroMissAuditEmitsNothing(t *testing.T) {
	t.Parallel()
	inst, reader := newAWSCloudRuntimeDriftInstruments(t)
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		NextSnapshot: map[string]string{"arn:keep": "p1", "arn:new": "p9"},
		LastAuditAt:  time.Now().Add(-25 * time.Hour),
		Now:          time.Now(),
	})
	if plan.Mode != AWSDriftIncrementalModeFull || plan.Reason != AWSDriftIncrementalReasonAuditDue {
		t.Fatalf("plan = %+v, want full/audit_due", plan)
	}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader: &stubAWSCloudRuntimeDriftEvidenceLoader{
			rows: deltaAuditFixtureRows(),
		},
		Writer:             &stubAWSCloudRuntimeDriftFindingWriter{},
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{7}},
		Instruments:        inst,
		IncrementalPlan:    &plan,
	}
	if _, err := handler.Handle(t.Context(), reducercontract.Intent{
		IntentID: "intent-audit-zero", ScopeID: "aws:scope", GenerationID: "gen-1",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := auditCorrectionsTotal(t, reader); got != -1 {
		t.Fatalf("audit corrections = %v, want absent (zero-miss audit)", got)
	}
}
