// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// transactionalFakeDB adds db.Beginner to fakeExecQueryer for ContentWriter
// tests only. Statements run inside a transaction are recorded in txExecs, not
// in the wrapped fake's execs, so the existing exact-count assertions on
// non-transactional content statements are unchanged by the infra inventory
// derive step. The shared fakeExecQueryer is deliberately left without Begin:
// several stores branch on db.Beginner and their tests rely on the
// non-transactional path.
type transactionalFakeDB struct {
	*fakeExecQueryer
	txMu      sync.Mutex
	txExecs   []fakeExecCall
	commits   int
	rollbacks int
	txExecErr error
}

func withTransactions(inner *fakeExecQueryer) *transactionalFakeDB {
	return &transactionalFakeDB{fakeExecQueryer: inner}
}

func (d *transactionalFakeDB) Begin(context.Context) (db.Transaction, error) {
	return &transactionalFakeTx{parent: d}, nil
}

type transactionalFakeTx struct{ parent *transactionalFakeDB }

func (tx *transactionalFakeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	tx.parent.txMu.Lock()
	defer tx.parent.txMu.Unlock()
	tx.parent.txExecs = append(tx.parent.txExecs, fakeExecCall{query: query, args: args})
	if tx.parent.txExecErr != nil {
		return nil, tx.parent.txExecErr
	}
	return fakeResult{}, nil
}

func (tx *transactionalFakeTx) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("transactionalFakeTx: unexpected query")
}

func (tx *transactionalFakeTx) Commit() error {
	tx.parent.txMu.Lock()
	defer tx.parent.txMu.Unlock()
	tx.parent.commits++
	return nil
}

func (tx *transactionalFakeTx) Rollback() error {
	tx.parent.txMu.Lock()
	defer tx.parent.txMu.Unlock()
	tx.parent.rollbacks++
	return nil
}

func TestContentWriterWriteDerivesInfraInventoryForEveryTouchedPath(t *testing.T) {
	t.Parallel()

	inner := &fakeExecQueryer{}
	database := withTransactions(inner)
	writer := NewContentWriter(database)

	mat := content.Materialization{
		RepoID:       "repo-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Records: []content.Record{
			{Path: "a.tf", Body: "resource {}"},
			{Path: "b.tf", Deleted: true},
		},
		Entities: []content.EntityRecord{
			{EntityID: "e1", Path: "c.tf", EntityType: "TerraformResource", EntityName: "r", StartLine: 1},
			{EntityID: "e2", Path: "d.tf", EntityType: "TerraformResource", EntityName: "gone", StartLine: 1, Deleted: true},
		},
	}
	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Two transactions: the tombstoned entity ids first (lock + delete by
	// entity_id), then the touched paths (lock + delete + insert).
	if got, want := len(database.txExecs), 5; got != want {
		t.Fatalf("derive statements = %d, want id lock+delete then path lock+delete+insert", got)
	}
	if database.commits != 2 || database.rollbacks != 0 {
		t.Fatalf("commits=%d rollbacks=%d, want two committed derive transactions", database.commits, database.rollbacks)
	}
	idDelete := database.txExecs[1]
	if !strings.Contains(idDelete.query, "entity_id = ANY") {
		t.Fatalf("tombstone statement = %q, want delete by entity_id", idDelete.query)
	}
	if got, want := []string(idDelete.args[1].(pgarray.StringArray)), []string{"e2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tombstoned ids = %v, want %v", got, want)
	}
	database.txExecs = database.txExecs[2:]
	deleteCall := database.txExecs[1]
	if !strings.Contains(deleteCall.query, "DELETE FROM infra_resource_entities") {
		t.Fatalf("second derive statement = %q, want the chunk delete", deleteCall.query)
	}
	if got, want := []string(deleteCall.args[1].(pgarray.StringArray)), []string{"a.tf", "b.tf", "c.tf", "d.tf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("derived paths = %v, want every touched record and entity path %v", got, want)
	}
	insertCall := database.txExecs[2]
	if insertCall.args[2] != "scope-1" || insertCall.args[3] != "gen-1" {
		t.Fatalf("derive provenance = %v/%v, want scope-1/gen-1", insertCall.args[2], insertCall.args[3])
	}

	// The derive runs after every content_entities statement, so it reads the
	// committed content state this Write produced.
	for _, call := range inner.execs {
		if strings.Contains(call.query, "infra_resource_entities") {
			t.Fatalf("derive statement ran outside the derive transaction: %q", call.query)
		}
	}
}

func TestContentWriterWriteFailsWhenInfraInventoryDeriveFails(t *testing.T) {
	t.Parallel()

	database := withTransactions(&fakeExecQueryer{})
	database.txExecErr = errors.New("lock timeout")
	writer := NewContentWriter(database)

	_, err := writer.Write(context.Background(), content.Materialization{
		RepoID:  "repo-1",
		Records: []content.Record{{Path: "a.tf", Body: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "infra inventory") {
		t.Fatalf("Write() error = %v, want the derive failure surfaced", err)
	}
	if database.rollbacks != 1 {
		t.Fatalf("rollbacks = %d, want the failed derive rolled back", database.rollbacks)
	}
}

// deriveOutcomes collects eshu_dp_infra_inventory_derives_total by outcome.
func deriveOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_infra_inventory_derives_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := point.Attributes.Value("outcome")
				out[outcome.AsString()] += point.Value
			}
		}
	}
	return out
}

func newDeriveMeter(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("infra-inventory-derive-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// TestContentWriterSkipsInfraDeriveWhenReadModelNotInstalled covers a writer
// binary that runs before its release's migration 109 (any deploy order other
// than the Helm pre-upgrade hook). The content statements already committed;
// failing the whole Write would dead-letter every projection for the window
// even though nothing reads the missing table. Readers stay on the graph until
// the backfill marker exists, and the backfill re-derives every repository
// once the table does, so skipping is safe. It is counted, never silent.
func TestContentWriterSkipsInfraDeriveWhenReadModelNotInstalled(t *testing.T) {
	t.Parallel()

	instruments, reader := newDeriveMeter(t)
	database := withTransactions(&fakeExecQueryer{})
	database.txExecErr = &pgconn.PgError{Code: "42P01", Message: `relation "infra_resource_entities" does not exist`}
	writer := NewContentWriter(database).WithInstruments(instruments)

	if _, err := writer.Write(context.Background(), content.Materialization{
		RepoID:  "repo-1",
		Records: []content.Record{{Path: "a.tf", Body: "x"}},
	}); err != nil {
		t.Fatalf("Write() error = %v, want the content write to succeed without the read model", err)
	}
	if got := deriveOutcomes(t, reader); got["skipped_not_installed"] != 1 || got["error"] != 0 {
		t.Fatalf("derive outcomes = %v, want one skipped_not_installed", got)
	}
}

func TestContentWriterCountsInfraDeriveOutcomes(t *testing.T) {
	t.Parallel()

	instruments, reader := newDeriveMeter(t)
	ok := NewContentWriter(withTransactions(&fakeExecQueryer{})).WithInstruments(instruments)
	if _, err := ok.Write(context.Background(), content.Materialization{
		RepoID: "repo-1", Records: []content.Record{{Path: "a.tf", Body: "x"}},
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	failing := withTransactions(&fakeExecQueryer{})
	failing.txExecErr = errors.New("lock timeout")
	if _, err := NewContentWriter(failing).WithInstruments(instruments).Write(context.Background(), content.Materialization{
		RepoID: "repo-1", Records: []content.Record{{Path: "a.tf", Body: "x"}},
	}); err == nil {
		t.Fatal("Write() error = nil, want the non-installation derive failure surfaced")
	}
	if got := deriveOutcomes(t, reader); got["ok"] != 1 || got["error"] != 1 {
		t.Fatalf("derive outcomes = %v, want ok=1 error=1", got)
	}
}
