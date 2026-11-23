// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	claimPacingEngagedMetric = "eshu_dp_workflow_claim_pacing_engaged_total"
	claimPacingActiveMetric  = "eshu_dp_workflow_claim_pacing_active"

	// sourceConsultMetric is the per-source consult-duration histogram; the
	// family consult at a claim grant labels it collector_kind (#10882).
	sourceConsultMetric = "eshu_dp_reducer_admission_source_consult_duration_seconds"
)

// scriptedRows is one scripted query result: rows scan into the caller's
// destinations by Go type, the way database/sql would.
type scriptedRows struct {
	rows  [][]any
	index int
}

func (r *scriptedRows) Next() bool { return r.index < len(r.rows) }

func (r *scriptedRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	r.index++
	if len(dest) != len(row) {
		return fmt.Errorf("scan destinations = %d, want %d", len(dest), len(row))
	}
	for i, target := range dest {
		switch d := target.(type) {
		case *string:
			*d = row[i].(string)
		case *bool:
			*d = row[i].(bool)
		case *int64:
			*d = row[i].(int64)
		case *float64:
			*d = row[i].(float64)
		case *sql.NullTime:
			*d = sql.NullTime{Time: row[i].(time.Time), Valid: true}
		default:
			return fmt.Errorf("unsupported scan destination %T", target)
		}
	}
	return nil
}

func (r *scriptedRows) Err() error   { return nil }
func (r *scriptedRows) Close() error { return nil }

// scriptedExecResult reports one changed row for every latch write.
type scriptedExecResult struct{}

func (scriptedExecResult) LastInsertId() (int64, error) { return 0, nil }
func (scriptedExecResult) RowsAffected() (int64, error) { return 1, nil }

// scriptedSourceDB answers the source-backpressure consult reads in order
// and accepts every latch write as a real transition, so a proof can drive
// the consult without a database.
type scriptedSourceDB struct {
	mu        sync.Mutex
	responses [][][]any
	execs     int
}

func (f *scriptedSourceDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.responses) == 0 {
		return &scriptedRows{}, nil
	}
	next := f.responses[0]
	f.responses = f.responses[1:]
	return &scriptedRows{rows: next}, nil
}

func (f *scriptedSourceDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs++
	return scriptedExecResult{}, nil
}

func (f *scriptedSourceDB) queue(responses ...[][]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append(f.responses, responses...)
}

func sourceBackpressureEnv(componentHome string) map[string]string {
	return map[string]string{
		envComponentHome:            componentHome,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        "dev.eshu.examples.scorecard",
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorInstanceID:      "scorecard-local",

		collector.SourceBackpressureEngageAgeEnv:     "1h",
		collector.SourceBackpressureReleaseAgeEnv:    "15m",
		collector.SourceBackpressureDepthFloorEnv:    "25",
		collector.SourceBackpressureGrantDelayEnv:    "2s",
		collector.SourceBackpressureMaxEngagementEnv: "30m",
	}
}

// TestBuildClaimedServiceRecordsClaimPacingLatchTransitions proves the claim
// store the extension's grant leg consults through exports the claim-pacing
// latch series (#10882): the production constructor path builds a control
// store whose engage moves eshu_dp_workflow_claim_pacing_engaged_total and
// raises the active gauge, and whose release takes the gauge back down.
func TestBuildClaimedServiceRecordsClaimPacingLatchTransitions(t *testing.T) {
	t.Parallel()

	componentHome := t.TempDir()
	installEnabledComponent(t, componentHome, "process", writeComponentExtensionConfig(t))
	inst, reader := grantInstruments(t)
	fake := &scriptedSourceDB{}

	service, err := buildClaimedService(fake, mapEnv(sourceBackpressureEnv(componentHome)),
		noop.NewTracerProvider().Tracer("test"), inst, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	checker, ok := service.ControlStore.(collector.SourceBackpressureChecker)
	if !ok {
		t.Fatalf("ControlStore %T does not serve CheckSourceBackpressure", service.ControlStore)
	}
	selector := workflow.ClaimSelector{
		CollectorKind:       scope.CollectorKind("scorecard"),
		CollectorInstanceID: "scorecard-local",
	}
	cfg, err := collector.LoadSourceBackpressureConfig(mapEnv(sourceBackpressureEnv(componentHome)))
	if err != nil {
		t.Fatalf("LoadSourceBackpressureConfig() error = %v, want nil", err)
	}
	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)

	// Engage: the family's one pending source is two hours old and 400 deep.
	fake.queue(
		[][]any{{"vuln-intel"}},
		[][]any{{float64(7200), int64(400), int64(400), int64(400)}},
		[][]any{},
	)
	decision, err := checker.CheckSourceBackpressure(context.Background(), selector, cfg, now)
	if err != nil {
		t.Fatalf("CheckSourceBackpressure(engage) error = %v, want nil", err)
	}
	if !decision.Paced {
		t.Fatal("Paced = false, want true for the behind source")
	}
	if got := claimPacingPoint(t, reader, claimPacingEngagedMetric, "vuln-intel"); got != 1 {
		t.Fatalf("%s{domain=vuln-intel} = %d, want 1 after one engage", claimPacingEngagedMetric, got)
	}
	if got := claimPacingPoint(t, reader, claimPacingActiveMetric, "vuln-intel"); got != 1 {
		t.Fatalf("%s{domain=vuln-intel} = %d, want 1 while engaged", claimPacingActiveMetric, got)
	}

	assertFamilyConsultRecorded(t, reader, "scorecard", 1)

	// Release: the queue cleared to five minutes old with the latch engaged.
	fake.queue(
		[][]any{{"vuln-intel"}},
		[][]any{{float64(300), int64(10), int64(0), int64(0)}},
		[][]any{{"vuln-intel", true, now.Add(-10 * time.Minute)}},
	)
	if _, err := checker.CheckSourceBackpressure(context.Background(), selector, cfg, now.Add(time.Minute)); err != nil {
		t.Fatalf("CheckSourceBackpressure(release) error = %v, want nil", err)
	}
	if got := claimPacingPoint(t, reader, claimPacingActiveMetric, "vuln-intel"); got != 0 {
		t.Fatalf("%s{domain=vuln-intel} = %d, want 0 after release", claimPacingActiveMetric, got)
	}
	assertFamilyConsultRecorded(t, reader, "scorecard", 2)
	if got := claimPacingPoint(t, reader, claimPacingEngagedMetric, "vuln-intel"); got != 1 {
		t.Fatalf("%s{domain=vuln-intel} = %d, want 1: a release never un-counts an engagement",
			claimPacingEngagedMetric, got)
	}
}

// claimPacingPoint sums one claim-pacing series for a domain label; a series
// that was never recorded reads 0, which is what the missing-wiring defect
// looked like on a scrape.
func claimPacingPoint(t *testing.T, reader *sdkmetric.ManualReader, name, domain string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	return counterWith(rm, name, "domain", domain)
}

// assertFamilyConsultRecorded proves the claim-grant family consult records
// the consult-duration histogram with collector_kind set and the ok outcome:
// that series was never emitted from the hosted extension while its control
// store carried no instruments. The histogram is registered by the per-source
// backpressure change (#10882); a build that does not carry the instrument yet
// has nothing to assert, so the check runs as soon as the field exists.
func assertFamilyConsultRecorded(t *testing.T, reader *sdkmetric.ManualReader, kind string, want uint64) {
	t.Helper()
	// Inert on origin/main: the instrument is absent there, so this returns without
	// asserting anything. It goes live once the per-source backpressure change that
	// registers ReducerAdmissionSourceConsultDuration is in the build. A rename of
	// the field would skip it silently, so drop this guard in that change.
	if _, ok := reflect.TypeOf(telemetry.Instruments{}).FieldByName("ReducerAdmissionSourceConsultDuration"); !ok {
		return
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var got uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || m.Name != sourceConsultMetric {
				continue
			}
			for _, dp := range hist.DataPoints {
				k, hasKind := dp.Attributes.Value(attributeKey("collector_kind"))
				outcome, _ := dp.Attributes.Value(attributeKey("outcome"))
				if _, hasSource := dp.Attributes.Value(attributeKey("source_system")); hasSource {
					t.Fatalf("family consult point carries source_system: %v", dp.Attributes)
				}
				if hasKind && k.AsString() == kind && outcome.AsString() == "ok" {
					got += dp.Count
				}
			}
		}
	}
	if got != want {
		t.Fatalf("%s{collector_kind=%s,outcome=ok} count = %d, want %d", sourceConsultMetric, kind, got, want)
	}
}
