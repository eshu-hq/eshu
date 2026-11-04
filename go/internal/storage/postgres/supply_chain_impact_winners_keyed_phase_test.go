// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// phaseFakeResult is the sql.Result of the fake transaction's Exec statements.
type phaseFakeResult struct{}

func (phaseFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (phaseFakeResult) RowsAffected() (int64, error) { return 0, nil }

// phaseFakeRows is one scripted row (or none) for a QueryContext statement.
type phaseFakeRows struct {
	row    []any
	served bool
}

func (r *phaseFakeRows) Next() bool {
	if r.row == nil || r.served {
		return false
	}
	r.served = true
	return true
}

func (r *phaseFakeRows) Scan(dest ...any) error {
	if len(dest) != len(r.row) {
		return fmt.Errorf("scan: %d destinations for %d values", len(dest), len(r.row))
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *bool:
			*p = r.row[i].(bool)
		case *string:
			*p = r.row[i].(string)
		case *int64:
			*p = r.row[i].(int64)
		case *sql.NullTime:
			*p = sql.NullTime{}
		default:
			return fmt.Errorf("scan: unsupported destination %T", d)
		}
	}
	return nil
}

func (*phaseFakeRows) Err() error   { return nil }
func (*phaseFakeRows) Close() error { return nil }

// phaseFakeTx scripts the keyed tick's statements by their SQL text. delay
// slows one statement and failOn fails one, so a test can prove the time of a
// failed phase is still recorded. rollbacks counts Rollback calls, so a test
// can prove the tick never leaks the transaction that holds the resweep lock.
type phaseFakeTx struct {
	claimed    bool
	consumed   int64
	identifier int64
	stored     int64
	delay      map[string]time.Duration
	failOn     string
	commitErr  error
	rollbacks  int
}

func (tx *phaseFakeTx) step(query string) error {
	time.Sleep(tx.delay[query])
	if tx.failOn == query {
		return errors.New("scripted statement failure")
	}
	return nil
}

func (tx *phaseFakeTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	if err := tx.step(query); err != nil {
		return nil, err
	}
	return phaseFakeResult{}, nil
}

func (tx *phaseFakeTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if err := tx.step(query); err != nil {
		return nil, err
	}
	switch query {
	case tryClaimSupplyChainImpactWinnersResweepSQL:
		return &phaseFakeRows{row: []any{tx.claimed}}, nil
	case countWinnersKeyedConsumedSQL:
		if tx.consumed == 0 {
			return &phaseFakeRows{}, nil
		}
		return &phaseFakeRows{row: []any{telemetry.SupplyChainImpactWinnersSourceActivation, tx.consumed}}, nil
	case countWinnersKeyedIdentsSQL:
		return &phaseFakeRows{row: []any{tx.identifier}}, nil
	case winnersKeyedStoredEstimateSQL:
		return &phaseFakeRows{row: []any{tx.stored}}, nil
	case stampWinnersWatermarkSQL:
		return &phaseFakeRows{row: []any{int64(0)}}, nil
	case applyWinnersKeyedSQL:
		return &phaseFakeRows{row: []any{int64(1), int64(0), int64(0), int64(0), int64(1), int64(0)}}, nil
	case rebuildSupplyChainImpactWinnersSQL:
		return &phaseFakeRows{row: []any{int64(1), int64(0), int64(0), int64(0), int64(0)}}, nil
	}
	return nil, fmt.Errorf("unscripted query %q", query)
}

func (tx *phaseFakeTx) Commit() error { return tx.commitErr }

func (tx *phaseFakeTx) Rollback() error {
	tx.rollbacks++
	return nil
}

// phaseFakeDB hands the one scripted transaction to ResweepWinnersKeyed, or
// fails Begin with beginErr.
type phaseFakeDB struct {
	tx       *phaseFakeTx
	beginErr error
}

func (phaseFakeDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("keyed tick must read through its transaction")
}

func (d phaseFakeDB) Begin(context.Context) (db.Transaction, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return d.tx, nil
}

// tickPhaseSamples returns the tick-phase histogram's sample count and summed
// seconds by phase label.
func tickPhaseSamples(t *testing.T, reader *sdkmetric.ManualReader) (counts map[string]uint64, seconds map[string]float64) {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	counts, seconds = map[string]uint64{}, map[string]float64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || m.Name != "eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds" {
				continue
			}
			for _, dp := range hist.DataPoints {
				phase, _ := dp.Attributes.Value("phase")
				counts[phase.AsString()] += dp.Count
				seconds[phase.AsString()] += dp.Sum
				if got := dp.Attributes.Len(); got != 1 {
					t.Errorf("tick phase sample has %d attributes, want only phase", got)
				}
			}
		}
	}
	return counts, seconds
}

func newPhaseStore(t *testing.T, tx *phaseFakeTx) (SupplyChainImpactWinnersStore, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return NewSupplyChainImpactWinnersStore(phaseFakeDB{tx: tx}).WithInstruments(inst), reader
}

// TestResweepWinnersKeyedRecordsOneSamplePerPhase pins #9975: a keyed tick
// records one duration sample for each phase it ran, under a closed phase label,
// so an operator can tell a slow read from a slow write. The phases a tick did
// not reach record nothing.
func TestResweepWinnersKeyedRecordsOneSamplePerPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tx   phaseFakeTx
		want []string
	}{
		{
			name: "keyed apply",
			tx:   phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100},
			want: []string{"claim", "consume", "build_idents", "build_active_facts", "apply", "commit"},
		},
		{
			name: "idle tick stamps the watermark",
			tx:   phaseFakeTx{claimed: true},
			want: []string{"claim", "consume", "stamp", "commit"},
		},
		{
			name: "over the cap falls back to the full rebuild",
			tx:   phaseFakeTx{claimed: true, consumed: 3, identifier: defaultWinnersKeyedMaxIdentifiers + 1, stored: 1000000},
			want: []string{"claim", "consume", "build_idents", "full_rebuild", "commit"},
		},
		{
			name: "lock held by another resweep",
			tx:   phaseFakeTx{claimed: false},
			want: []string{"claim"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := tt.tx
			store, reader := newPhaseStore(t, &tx)
			if _, err := store.ResweepWinnersKeyed(context.Background(), time.Now()); err != nil {
				t.Fatalf("ResweepWinnersKeyed() error = %v", err)
			}
			counts, _ := tickPhaseSamples(t, reader)
			want := map[string]uint64{}
			for _, phase := range tt.want {
				want[phase] = 1
			}
			if !reflect.DeepEqual(counts, want) {
				t.Errorf("tick phase samples = %v, want %v", counts, want)
			}
			// A tick that began a transaction rolls it back exactly once: after
			// a commit it is a no-op, and on a held lock it releases the claim.
			if tx.rollbacks != 1 {
				t.Errorf("Rollback calls = %d, want 1", tx.rollbacks)
			}
		})
	}
}

// TestResweepWinnersKeyedRecordsTheTimeOfAFailedPhase pins that a statement
// that fails still leaves its phase sample, with the time it spent, and that
// no later phase records anything. A tick that dies in the apply is the slow
// one an operator most needs to see.
func TestResweepWinnersKeyedRecordsTheTimeOfAFailedPhase(t *testing.T) {
	t.Parallel()

	const spent = 30 * time.Millisecond
	tests := []struct {
		name   string
		failOn string
		phase  string
		want   []string
	}{
		{"claim", tryClaimSupplyChainImpactWinnersResweepSQL, "claim", []string{"claim"}},
		{"consume", consumeWinnersDirtyLogSQL, "consume", []string{"claim", "consume"}},
		{"build_idents", buildWinnersKeyedIdentsSQL, "build_idents", []string{"claim", "consume", "build_idents"}},
		{"build_active_facts", addWinnersKeyedOperatorFactsSQL, "build_active_facts", []string{"claim", "consume", "build_idents", "build_active_facts"}},
		{"apply", applyWinnersKeyedSQL, "apply", []string{"claim", "consume", "build_idents", "build_active_facts", "apply"}},
		{"commit", "", "commit", []string{"claim", "consume", "build_idents", "build_active_facts", "apply", "commit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := &phaseFakeTx{claimed: true, consumed: 2, identifier: 2, stored: 100, failOn: tt.failOn}
			if tt.name == "commit" {
				tx.commitErr = errors.New("commit failed")
			}
			if tt.failOn != "" {
				tx.delay = map[string]time.Duration{tt.failOn: spent}
			}
			store, reader := newPhaseStore(t, tx)
			if _, err := store.ResweepWinnersKeyed(context.Background(), time.Now()); err == nil {
				t.Fatal("ResweepWinnersKeyed() error = nil, want the scripted failure")
			}
			counts, seconds := tickPhaseSamples(t, reader)
			want := map[string]uint64{}
			for _, phase := range tt.want {
				want[phase] = 1
			}
			if !reflect.DeepEqual(counts, want) {
				t.Errorf("tick phase samples = %v, want %v", counts, want)
			}
			if tt.failOn != "" && seconds[tt.phase] < spent.Seconds() {
				t.Errorf("failed phase %s recorded %.4f s, want at least %.4f s", tt.phase, seconds[tt.phase], spent.Seconds())
			}
			// A failure in any phase must still release the transaction, or it
			// would hold the resweep try-lock and a pooled connection.
			if tx.rollbacks != 1 {
				t.Errorf("Rollback calls = %d, want 1", tx.rollbacks)
			}
		})
	}
}

// TestResweepWinnersKeyedBeginFailureRollsBackNothing pins that a tick whose
// Begin fails owns no transaction: it records the claim time, returns the
// begin error, and never calls Rollback.
func TestResweepWinnersKeyedBeginFailureRollsBackNothing(t *testing.T) {
	t.Parallel()

	tx := &phaseFakeTx{claimed: true}
	beginErr := errors.New("begin refused")
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	store := NewSupplyChainImpactWinnersStore(phaseFakeDB{tx: tx, beginErr: beginErr}).WithInstruments(inst)
	if _, err := store.ResweepWinnersKeyed(context.Background(), time.Now()); !errors.Is(err, beginErr) {
		t.Fatalf("ResweepWinnersKeyed() error = %v, want %v", err, beginErr)
	}
	if tx.rollbacks != 0 {
		t.Errorf("Rollback calls = %d, want 0 when Begin failed", tx.rollbacks)
	}
	counts, _ := tickPhaseSamples(t, reader)
	if want := map[string]uint64{"claim": 1}; !reflect.DeepEqual(counts, want) {
		t.Errorf("tick phase samples = %v, want %v", counts, want)
	}
}

// TestResweepWinnersKeyedWithoutInstrumentsRunsUnchanged pins that a store
// with no instruments (every existing caller and test) behaves as before.
func TestResweepWinnersKeyedWithoutInstrumentsRunsUnchanged(t *testing.T) {
	t.Parallel()

	tx := &phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100}
	got, err := NewSupplyChainImpactWinnersStore(phaseFakeDB{tx: tx}).ResweepWinnersKeyed(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("ResweepWinnersKeyed() error = %v", err)
	}
	if got.Path != telemetry.SupplyChainImpactWinnersPathKeyed || got.Identifiers != 3 || got.Inserted != 1 || got.Keys != 1 {
		t.Errorf("result = %+v, want a keyed apply of 3 identifiers", got)
	}
}
