package postgres

import (
	"context"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newEnqueueInstruments builds a real telemetry.Instruments backed by an
// sdkmetric ManualReader so tests can assert counter advances without
// touching the global meter provider. Mirrors the same idiom in
// go/internal/reducer/terraform_config_state_drift_test.go; the helpers
// are duplicated locally because test helpers cannot cross package
// boundaries.
func newEnqueueInstruments(t *testing.T) (*telemetry.Instruments, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

// counterTotal sums every Int64 counter data point for the named metric in
// the collected ResourceMetrics. Used to assert per-test increments without
// resetting the reader between increments.
func counterTotal(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

func TestEnqueueConfigStateDriftIntentsEnqueuesOnePerActiveScope(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// listActiveStateSnapshotScopes returns two state_snapshot scopes
			// with active generations.
			{rows: [][]any{
				{"state_snapshot:s3:hash-1", "gen-state-1"},
				{"state_snapshot:s3:hash-2", "gen-state-2"},
			}},
		},
	}
	store := NewIngestionStore(db)

	if err := store.EnqueueConfigStateDriftIntents(context.Background(), nil, nil); err != nil {
		t.Fatalf("EnqueueConfigStateDriftIntents() error = %v, want nil", err)
	}

	// One QUERY for the scope scan + one EXEC (batch INSERT for both intents).
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "FROM ingestion_scopes") {
		t.Fatalf("query missing FROM ingestion_scopes: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "state_snapshot:%") {
		t.Fatalf("query missing state_snapshot prefix: %s", db.queries[0].query)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (single batch INSERT)", got, want)
	}
	insert := db.execs[0].query
	if !strings.Contains(insert, "INSERT INTO fact_work_items") {
		t.Fatalf("exec query missing fact_work_items insert: %s", insert)
	}
	// The reducer queue carries the domain string as one of the bound args
	// per row; assert it shows up in the argument slice.
	foundDomain := false
	for _, arg := range db.execs[0].args {
		if s, ok := arg.(string); ok && s == "config_state_drift" {
			foundDomain = true
			break
		}
	}
	if !foundDomain {
		t.Fatalf("config_state_drift domain not present in INSERT args: %#v", db.execs[0].args)
	}
}

func TestEnqueueConfigStateDriftIntentsNoOpWhenNoScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	store := NewIngestionStore(db)

	if err := store.EnqueueConfigStateDriftIntents(context.Background(), nil, nil); err != nil {
		t.Fatalf("EnqueueConfigStateDriftIntents() error = %v, want nil", err)
	}

	// Scope scan ran (1 query); no exec because there were no intents.
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 0; got != want {
		t.Fatalf("exec count = %d, want %d (no intents to enqueue)", got, want)
	}
}

func TestEnqueueConfigStateDriftIntentsRequiresDatabase(t *testing.T) {
	t.Parallel()

	var store IngestionStore
	if err := store.EnqueueConfigStateDriftIntents(context.Background(), nil, nil); err == nil {
		t.Fatal("nil DB: error = nil, want non-nil")
	}
}

func TestEnqueueConfigStateDriftIntentsAdvancesEnqueueCounterByScopeCount(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"state_snapshot:s3:hash-1", "gen-state-1"},
				{"state_snapshot:s3:hash-2", "gen-state-2"},
				{"state_snapshot:s3:hash-3", "gen-state-3"},
			}},
		},
	}
	store := NewIngestionStore(db)

	inst, reader := newEnqueueInstruments(t)

	if err := store.EnqueueConfigStateDriftIntents(context.Background(), nil, inst); err != nil {
		t.Fatalf("EnqueueConfigStateDriftIntents() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got, want := counterTotal(rm, "eshu_dp_correlation_drift_intents_enqueued_total"), int64(3); got != want {
		t.Fatalf("enqueue counter = %d, want %d", got, want)
	}
}

func TestEnqueueConfigStateDriftIntentsCounterEmitsZeroWhenNoScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	store := NewIngestionStore(db)

	inst, reader := newEnqueueInstruments(t)

	if err := store.EnqueueConfigStateDriftIntents(context.Background(), nil, inst); err != nil {
		t.Fatalf("EnqueueConfigStateDriftIntents() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	// Zero scopes means the counter advances by 0, but the series MUST exist
	// so dashboards can distinguish "Phase 3.5 ran and produced zero work"
	// from "Phase 3.5 did not run." Asserting the series is registered via
	// counterTotal returning a defined zero (not a missing-series panic).
	if got, want := counterTotal(rm, "eshu_dp_correlation_drift_intents_enqueued_total"), int64(0); got != want {
		t.Fatalf("enqueue counter = %d, want %d (no-op trigger)", got, want)
	}
}
