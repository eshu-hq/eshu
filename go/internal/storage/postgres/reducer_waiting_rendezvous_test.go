// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// rendezvousRows is a one-column text result.
type rendezvousRows struct {
	values []string
	index  int
}

func (r *rendezvousRows) Next() bool { return r.index < len(r.values) }

func (r *rendezvousRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("scan destinations = %d, want 1", len(dest))
	}
	target, ok := dest[0].(*string)
	if !ok {
		return fmt.Errorf("scan destination = %T, want *string", dest[0])
	}
	*target = r.values[r.index]
	r.index++
	return nil
}

func (r *rendezvousRows) Err() error   { return nil }
func (r *rendezvousRows) Close() error { return nil }

// rendezvousFake scripts the two park-side rendezvous statements: the
// non-blocking try (a query that returns the keys currently held exclusively)
// and the single-key blocking wait (an exec).
type rendezvousFake struct {
	mu sync.Mutex
	// busy are the keys the try statement reports as held exclusively.
	busy map[string]bool
	// tryErr and blockErr fail the matching statement. blockFn, when set,
	// replaces the blocking statement's behavior.
	tryErr, blockErr error
	blockFn          func(ctx context.Context, key string) error
	tryBatches       [][]string
	blocked          []string
}

func (f *rendezvousFake) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	if query != tryBlockingActivationsQuery {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	keys := []string(args[0].(array.StringArray))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tryBatches = append(f.tryBatches, keys)
	if f.tryErr != nil {
		return nil, f.tryErr
	}
	var busy []string
	for _, key := range keys {
		if f.busy[key] {
			busy = append(busy, key)
		}
	}
	return &rendezvousRows{values: busy}, nil
}

func (f *rendezvousFake) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if query != awaitBlockingActivationQuery {
		return nil, fmt.Errorf("unexpected exec: %s", query)
	}
	key, _ := args[0].(string)
	f.mu.Lock()
	f.blocked = append(f.blocked, key)
	fn, err := f.blockFn, f.blockErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if fn != nil {
		if err := fn(ctx, key); err != nil {
			return nil, err
		}
	}
	return fakeResult{}, nil
}

func rendezvousKeyJSON(t *testing.T, blocking []string) string {
	t.Helper()
	keyJSON, ok := narrowedWaitingKey("scope-waiter", "gen-waiter", blocking, []string{"repo-1"})
	if !ok {
		t.Fatal("narrowedWaitingKey refused the key")
	}
	return keyJSON
}

func rendezvousMetrics(t *testing.T) (*telemetry.Instruments, func(outcome string) int64) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("rendezvous"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return instruments, func(outcome string) int64 {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("collect: %v", err)
		}
		var total int64
		for _, scope := range rm.ScopeMetrics {
			for _, m := range scope.Metrics {
				sum, ok := m.Data.(metricdata.Sum[int64])
				if m.Name != "eshu_dp_reducer_waiting_wake_total" || !ok {
					continue
				}
				for _, point := range sum.DataPoints {
					site, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionWaitingWakeSite))
					got, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
					if site.AsString() == telemetry.WaitingWakeSiteParkRendezvous && got.AsString() == outcome {
						total += point.Value
					}
				}
			}
		}
		return total
	}
}

func TestAwaitInFlightActivationsFreeParkCostsOneStatementAndRecordsNothing(t *testing.T) {
	t.Parallel()
	fake := &rendezvousFake{}
	instruments, count := rendezvousMetrics(t)
	err := awaitInFlightActivations(context.Background(), fake, instruments,
		WaitingKindRelationshipGenerationActive, rendezvousKeyJSON(t, []string{"scope-a", "scope-b"}))
	if err != nil {
		t.Fatalf("awaitInFlightActivations: %v", err)
	}
	if got := len(fake.tryBatches); got != 1 {
		t.Fatalf("try statements = %d, want 1", got)
	}
	if len(fake.blocked) != 0 {
		t.Fatalf("blocking statements = %v, want none", fake.blocked)
	}
	for _, outcome := range []string{telemetry.WaitingWakeOutcomeWaited, telemetry.WaitingWakeOutcomeTimeout, telemetry.WaitingWakeOutcomeError} {
		if got := count(outcome); got != 0 {
			t.Fatalf("park_rendezvous %s = %d, want 0 for a free park", outcome, got)
		}
	}
}

func TestAwaitInFlightActivationsBoundsLocksPerStatement(t *testing.T) {
	t.Parallel()
	blocking := make([]string, 0, 3*rendezvousTryChunkSize+7)
	for i := range 3*rendezvousTryChunkSize + 7 {
		blocking = append(blocking, fmt.Sprintf("scope-%05d", i))
	}
	fake := &rendezvousFake{}
	if err := awaitInFlightActivations(context.Background(), fake, nil,
		WaitingKindRelationshipGenerationActive, rendezvousKeyJSON(t, blocking)); err != nil {
		t.Fatalf("awaitInFlightActivations: %v", err)
	}
	seen := 0
	for _, batch := range fake.tryBatches {
		if len(batch) > rendezvousTryChunkSize {
			t.Fatalf("a try statement took %d locks, want at most %d", len(batch), rendezvousTryChunkSize)
		}
		seen += len(batch)
	}
	if seen != len(blocking) {
		t.Fatalf("try statements covered %d keys, want %d: every blocking scope needs its own check", seen, len(blocking))
	}
}

func TestAwaitInFlightActivationsWaitsOneKeyPerStatementAndRecordsWaited(t *testing.T) {
	t.Parallel()
	busyA := activationRendezvousKey("scope-b")
	busyB := activationRendezvousKey("scope-d")
	fake := &rendezvousFake{busy: map[string]bool{busyA: true, busyB: true}}
	instruments, count := rendezvousMetrics(t)
	err := awaitInFlightActivations(context.Background(), fake, instruments,
		WaitingKindRelationshipGenerationActive,
		rendezvousKeyJSON(t, []string{"scope-a", "scope-b", "scope-c", "scope-d"}))
	if err != nil {
		t.Fatalf("awaitInFlightActivations: %v", err)
	}
	if got, want := strings.Join(fake.blocked, ","), busyA+","+busyB; got != want {
		t.Fatalf("blocking statements = %q, want one per busy key, in order: %q", got, want)
	}
	if got := count(telemetry.WaitingWakeOutcomeWaited); got != 1 {
		t.Fatalf("park_rendezvous waited = %d, want 1 per park", got)
	}
}

func TestAwaitInFlightActivationsTimeoutParksAndRecordsTimeout(t *testing.T) {
	t.Parallel()
	busy := activationRendezvousKey("scope-a")
	fake := &rendezvousFake{
		busy: map[string]bool{busy: true},
		blockFn: func(ctx context.Context, _ string) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	instruments, count := rendezvousMetrics(t)
	started := time.Now()
	err := awaitInFlightActivations(context.Background(), fake, instruments,
		WaitingKindRelationshipGenerationActive, rendezvousKeyJSON(t, []string{"scope-a"}))
	if err != nil {
		t.Fatalf("awaitInFlightActivations = %v, want nil: a timed-out rendezvous parks and the audit bounds the wake", err)
	}
	if took := time.Since(started); took < narrowedParkRendezvousTimeout || took > narrowedParkRendezvousTimeout+2*time.Second {
		t.Fatalf("rendezvous gave up after %s, want about %s", took, narrowedParkRendezvousTimeout)
	}
	if got := count(telemetry.WaitingWakeOutcomeTimeout); got != 1 {
		t.Fatalf("park_rendezvous timeout = %d, want 1", got)
	}
	if got := count(telemetry.WaitingWakeOutcomeWaited); got != 0 {
		t.Fatalf("park_rendezvous waited = %d, want 0 after a timeout", got)
	}
}

func TestAwaitInFlightActivationsStatementErrorParksAndRecordsError(t *testing.T) {
	t.Parallel()
	for name, fake := range map[string]*rendezvousFake{
		"try statement":   {tryErr: errors.New("out of shared memory")},
		"block statement": {busy: map[string]bool{activationRendezvousKey("scope-a"): true}, blockErr: errors.New("connection reset")},
	} {
		t.Run(name, func(t *testing.T) {
			instruments, count := rendezvousMetrics(t)
			err := awaitInFlightActivations(context.Background(), fake, instruments,
				WaitingKindRelationshipGenerationActive, rendezvousKeyJSON(t, []string{"scope-a"}))
			if err != nil {
				t.Fatalf("awaitInFlightActivations = %v, want nil: the wait row is already parked and the audit bounds a missed wake; an error here would stop the reducer", err)
			}
			if got := count(telemetry.WaitingWakeOutcomeError); got != 1 {
				t.Fatalf("park_rendezvous error = %d, want 1", got)
			}
		})
	}
}

func TestAwaitInFlightActivationsCallerCancellationIsReturned(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &rendezvousFake{tryErr: context.Canceled}
	instruments, count := rendezvousMetrics(t)
	err := awaitInFlightActivations(ctx, fake, instruments,
		WaitingKindRelationshipGenerationActive, rendezvousKeyJSON(t, []string{"scope-a"}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("awaitInFlightActivations = %v, want the caller's cancellation: shutdown is not a rendezvous failure", err)
	}
	for _, outcome := range []string{telemetry.WaitingWakeOutcomeTimeout, telemetry.WaitingWakeOutcomeError} {
		if got := count(outcome); got != 0 {
			t.Fatalf("park_rendezvous %s = %d, want 0 on shutdown", outcome, got)
		}
	}
}

func TestAwaitInFlightActivationsSkipsKindsWithOtherProviders(t *testing.T) {
	t.Parallel()
	fake := &rendezvousFake{}
	for _, kind := range []WaitingWakeKind{WaitingKindPhaseCommitted, WaitingKindProducerTerminal} {
		if err := awaitInFlightActivations(context.Background(), fake, nil, kind,
			rendezvousKeyJSON(t, []string{"scope-a"})); err != nil {
			t.Fatalf("awaitInFlightActivations(%s): %v", kind, err)
		}
	}
	if len(fake.tryBatches) != 0 || len(fake.blocked) != 0 {
		t.Fatalf("statements for kinds with other providers: tries %d, blocks %v, want none", len(fake.tryBatches), fake.blocked)
	}
}
