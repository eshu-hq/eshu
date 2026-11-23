// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// latchWriteSpy wraps one process's database handle and records every
// latch write the store issues together with the rows the statement
// changed, so a proof can tell a statement that moved the latch from a
// racing loser that matched nothing.
type latchWriteSpy struct {
	db.ExecQueryer

	mu     sync.Mutex
	writes []int64
}

func (s *latchWriteSpy) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := s.ExecQueryer.ExecContext(ctx, query, args...)
	if err == nil && query == claimPacingLatchUpsert {
		affected, _ := result.RowsAffected()
		s.mu.Lock()
		s.writes = append(s.writes, affected)
		s.mu.Unlock()
	}
	return result, err
}

// changed counts the latch writes that changed a row.
func (s *latchWriteSpy) changed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, affected := range s.writes {
		if affected > 0 {
			count++
		}
	}
	return count
}

// latchConsumer is one simulated process of the fleet: its own connection
// pool, its own store, its own meter provider (one process's metrics are
// the series one scrape target exports), and its own consult clock.
type latchConsumer struct {
	store  *WorkflowControlStore
	reader *sdkmetric.ManualReader
	spy    *latchWriteSpy
	now    time.Time
}

// isolatedDatabaseDSN returns a DSN for the disposable database behind
// database, so a proof can open independent pools onto it.
func isolatedDatabaseDSN(t *testing.T, database *sql.DB) string {
	t.Helper()
	var name string
	if err := database.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatalf("read current_database(): %v", err)
	}
	dsn, err := postgresproof.WithDatabase(os.Getenv("ESHU_POSTGRES_DSN"), name)
	if err != nil {
		t.Fatalf("derive disposable DSN: %v", err)
	}
	return dsn
}

// newLatchConsumers builds n simulated fleet processes onto the database
// behind database. Each consult clock starts at base and steps by one
// millisecond, so the row's engaged_at names which consult wrote it.
func newLatchConsumers(t *testing.T, database *sql.DB, n int, base time.Time) []*latchConsumer {
	t.Helper()
	dsn := isolatedDatabaseDSN(t, database)
	consumers := make([]*latchConsumer, 0, n)
	for i := 0; i < n; i++ {
		pool, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatalf("open consumer pool %d: %v", i, err)
		}
		pool.SetMaxOpenConns(4)
		t.Cleanup(func() { _ = pool.Close() })
		spy := &latchWriteSpy{ExecQueryer: SQLDB{DB: pool}}
		reader := sdkmetric.NewManualReader()
		inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("latch-race"))
		if err != nil {
			t.Fatalf("NewInstruments() error = %v, want nil", err)
		}
		store := NewWorkflowControlStore(spy)
		store.Instruments = inst
		consumers = append(consumers, &latchConsumer{
			store:  store,
			reader: reader,
			spy:    spy,
			now:    base.Add(time.Duration(i) * time.Millisecond).UTC().Truncate(time.Microsecond),
		})
	}
	return consumers
}

// raceAll runs fn for every consumer at once, released by one barrier so
// the consults overlap on the shared latch row.
func raceAll(t *testing.T, consumers []*latchConsumer, fn func(c *latchConsumer) error) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, len(consumers))
	var wg sync.WaitGroup
	for _, c := range consumers {
		wg.Add(1)
		go func(c *latchConsumer) {
			defer wg.Done()
			<-start
			errs <- fn(c)
		}(c)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("racing consult error = %v, want nil", err)
		}
	}
}

// pacingMetric reads one consumer's pacing series for a signal: the sum
// recorded for pacing_signal=signal, 0 when the series was never written.
func (c *latchConsumer) pacingMetric(t *testing.T, name, signal string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := c.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", name, m.Data)
			}
			for _, p := range sum.DataPoints {
				got, _ := p.Attributes.Value(attribute.Key("pacing_signal"))
				if got.AsString() == signal {
					total += p.Value
				}
			}
		}
	}
	return total
}

// fleetPacingSum sums one pacing series for a signal across every
// consumer, the fleet-wide value a dashboard sum() reports.
func fleetPacingSum(t *testing.T, consumers []*latchConsumer, name, signal string) int64 {
	t.Helper()
	var total int64
	for _, c := range consumers {
		total += c.pacingMetric(t, name, signal)
	}
	return total
}

// fleetChangedWrites counts the latch writes that changed a row across the
// fleet.
func fleetChangedWrites(consumers []*latchConsumer) int {
	total := 0
	for _, c := range consumers {
		total += c.spy.changed()
	}
	return total
}

// latchRowSnapshot is one latch row as storage holds it.
type latchRowSnapshot struct {
	engaged   bool
	engagedAt sql.NullTime
	updatedAt time.Time
	xmin      string
}

// readLatchRow reads one latch row, including its xmin so a proof can see
// whether any statement wrote a new row version.
func readLatchRow(t *testing.T, database *sql.DB, signal, domain string) latchRowSnapshot {
	t.Helper()
	var row latchRowSnapshot
	err := database.QueryRow(`
SELECT engaged, engaged_at, updated_at, xmin::text
FROM workflow_claim_pacing WHERE signal = $1 AND domain = $2`, signal, domain).
		Scan(&row.engaged, &row.engagedAt, &row.updatedAt, &row.xmin)
	if err != nil {
		t.Fatalf("read latch row %s/%s: %v", signal, domain, err)
	}
	return row
}

const (
	pacingEngagedTotalMetric = "eshu_dp_workflow_claim_pacing_engaged_total"
	pacingActiveMetric       = "eshu_dp_workflow_claim_pacing_active"
)
