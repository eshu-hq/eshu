// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// wakeScriptDB answers the activation transaction and the queue's Fail with
// scripted results: the wake statement returns wakeRows or wakeErr; every
// other statement succeeds on one row.
type wakeScriptDB struct {
	wakeRows int64
	wakeErr  error
}

func (d *wakeScriptDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	if query == wakeProducerTerminalConsumersSQL {
		if d.wakeErr != nil {
			return nil, d.wakeErr
		}
		return fakeResultWithRowsAffected{rowsAffected: d.wakeRows}, nil
	}
	return fakeResultWithRowsAffected{rowsAffected: 1}, nil
}

func (d *wakeScriptDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return newRelationshipRows(nil), nil
}

func (d *wakeScriptDB) Begin(context.Context) (db.Transaction, error) { return wakeScriptTx{d}, nil }

type wakeScriptTx struct{ *wakeScriptDB }

func (wakeScriptTx) Commit() error   { return nil }
func (wakeScriptTx) Rollback() error { return nil }

func newWakeMetrics(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("wake"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return instruments, reader
}

// wakeCount reads eshu_dp_reducer_own_gate_wake_total for one site/outcome.
func wakeCount(t *testing.T, reader *sdkmetric.ManualReader, site, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != "eshu_dp_reducer_own_gate_wake_total" || !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				gotSite, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSite))
				gotOutcome, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				if gotSite.AsString() == site && gotOutcome.AsString() == outcome {
					total += point.Value
				}
			}
		}
	}
	return total
}

func TestActivationWakeCountsEveryOutcome(t *testing.T) {
	t.Parallel()
	instruments, reader := newWakeMetrics(t)
	for _, step := range []struct {
		db      *wakeScriptDB
		wantErr bool
	}{
		{db: &wakeScriptDB{wakeRows: 2}},
		{db: &wakeScriptDB{wakeRows: 0}},
		{db: &wakeScriptDB{wakeErr: errors.New("wake failed")}, wantErr: true},
	} {
		err := NewRelationshipStore(step.db).WithInstruments(instruments).
			ActivateResolutionGeneration(context.Background(), "gen-1", "scope-1")
		if (err != nil) != step.wantErr {
			t.Fatalf("activation error = %v, want error %v", err, step.wantErr)
		}
	}
	for outcome, want := range map[string]int64{
		telemetry.OwnGateWakeOutcomeReopened: 1, telemetry.OwnGateWakeOutcomeNoop: 1, telemetry.OwnGateWakeOutcomeError: 1,
	} {
		if got := wakeCount(t, reader, telemetry.OwnGateWakeSiteActivation, outcome); got != want {
			t.Fatalf("activation %s = %d, want %d", outcome, got, want)
		}
	}
}

func TestFailSideWakeCountsEveryOutcome(t *testing.T) {
	t.Parallel()
	instruments, reader := newWakeMetrics(t)
	dead := crossrepo.EvaluateOwnGate(context.Background(), func(string) (bool, error) { return false, nil },
		func(context.Context, string, string) (crossrepo.ProducerState, error) {
			return crossrepo.ProducerState{ScopeActiveGenerationID: "gen-1", WorkItemID: "dm-1", Status: "dead_letter"}, nil
		}, reducercontract.Intent{ScopeID: "scope-1", GenerationID: "gen-1", Domain: reducer.DomainDeployableUnitCorrelation},
		true, time.Now()).Err
	for _, script := range []*wakeScriptDB{
		{wakeRows: 1}, {wakeRows: 0}, {wakeErr: errors.New("wake failed")},
	} {
		q := ReducerQueue{
			database: script, LeaseOwner: "w", LeaseDuration: time.Minute, MaxAttempts: 3,
			Instruments: instruments, Now: time.Now,
		}
		intent := reducercontract.Intent{IntentID: "c-1", ScopeID: "scope-1", GenerationID: "gen-1", AttemptCount: 3}
		if err := q.Fail(context.Background(), intent, dead); err != nil {
			t.Fatalf("Fail: %v (a wake failure must not fail the committed dead letter)", err)
		}
	}
	for outcome, want := range map[string]int64{
		telemetry.OwnGateWakeOutcomeReopened: 1, telemetry.OwnGateWakeOutcomeNoop: 1, telemetry.OwnGateWakeOutcomeError: 1,
	} {
		if got := wakeCount(t, reader, telemetry.OwnGateWakeSiteFail, outcome); got != want {
			t.Fatalf("fail-side %s = %d, want %d", outcome, got, want)
		}
	}
}
