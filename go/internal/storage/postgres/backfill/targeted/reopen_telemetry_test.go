// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type reopenProofRows struct {
	data [][]string
	at   int
}

func (r *reopenProofRows) Next() bool { return r.at < len(r.data) }
func (r *reopenProofRows) Scan(dest ...any) error {
	row := r.data[r.at]
	r.at++
	if len(dest) != len(row) {
		return fmt.Errorf("scan columns = %d, want %d", len(dest), len(row))
	}
	for i, v := range row {
		ptr, ok := dest[i].(*string)
		if !ok {
			return fmt.Errorf("scan destination %d type %T", i, dest[i])
		}
		*ptr = v
	}
	return nil
}
func (*reopenProofRows) Err() error   { return nil }
func (*reopenProofRows) Close() error { return nil }

type reopenProofTx struct {
	responses  []*reopenProofRows
	queries    int
	committed  bool
	rolledBack bool
}

func (x *reopenProofTx) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	if x.queries >= len(x.responses) {
		return nil, fmt.Errorf("unexpected reopen query %d", x.queries)
	}
	r := x.responses[x.queries]
	x.queries++
	return r, nil
}

func (*reopenProofTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected exec")
}
func (x *reopenProofTx) Commit() error   { x.committed = true; return nil }
func (x *reopenProofTx) Rollback() error { x.rolledBack = true; return nil }

type reopenProofBeginner struct {
	tx     *reopenProofTx
	begins int
}

func (b *reopenProofBeginner) Begin(context.Context) (db.Transaction, error) {
	b.begins++
	return b.tx, nil
}

func TestTargetedReopenEmitsOneTaggedBlockPerDomain(t *testing.T) {
	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })
	var output bytes.Buffer
	log.SetOutput(&output)
	affected := []backfill.ScopeGenerationPartition{{ScopeID: "scope-a", GenerationID: "gen-a"}, {ScopeID: "scope-b", GenerationID: "gen-b"}}
	skipped := map[backfill.ScopeGenerationPartition]struct{}{affected[0]: {}}
	tx := &reopenProofTx{responses: []*reopenProofRows{
		{data: [][]string{{"wi-1", "scope-a", "gen-a"}, {"wi-2", "scope-b", "gen-b"}}},
		{},
		{data: [][]string{{"wi-c1"}, {"wi-c2"}}},
		{},
	}}
	beginner := &reopenProofBeginner{tx: tx}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	domains := []string{"correlation-a", "correlation-b"}
	var reopenedIDs []string
	store := Store{
		Backfill: backfill.Store{Beginner: beginner}, Queries: ProductionQueries, CorrelationDomains: domains,
		GateReopen: func(_ context.Context, _ string, items []WorkItem, skip map[backfill.ScopeGenerationPartition]struct{}) ([]string, int) {
			var ids []string
			n := 0
			for _, item := range items {
				if _, ok := skip[item.Partition]; ok {
					n++
					continue
				}
				ids = append(ids, item.WorkItemID)
			}
			return ids, n
		},
		ReopenSucceeded: func(_ context.Context, got db.Transaction, id string) (bool, error) {
			if got != tx {
				t.Fatal("reopen callback received a different transaction")
			}
			reopenedIDs = append(reopenedIDs, id)
			return true, nil
		},
	}
	reopened, err := store.ReopenTargetedMaintenanceWorkItems(context.Background(), provider.Tracer("proof"), affected, skipped)
	if err != nil {
		t.Fatal(err)
	}
	if beginner.begins != 1 || !tx.committed || tx.rolledBack || tx.queries != 4 {
		t.Fatalf("transaction: begins=%d commit=%t rollback=%t queries=%d", beginner.begins, tx.committed, tx.rolledBack, tx.queries)
	}
	if reopened["deployment_mapping"] != 1 || reopened[domains[0]] != 2 || !reflect.DeepEqual(reopenedIDs, []string{"wi-2", "wi-c1", "wi-c2"}) {
		t.Fatalf("reopened=%v ids=%v", reopened, reopenedIDs)
	}
	spans := recorder.Ended()
	if len(spans) != 5 {
		t.Fatalf("span count=%d, want 5", len(spans))
	}
	wantDomains := []string{"deployment_mapping", "code_import_repo_edge", domains[0], domains[1]}
	wantReopened := []int64{1, 0, 2, 0}
	wantSkipped := []int64{1, 0, 0, 0}
	for i, span := range spans[:4] {
		if span.Name() != "relationship.backfill.reopen" {
			t.Errorf("span[%d]=%s", i, span.Name())
		}
		attrs := map[string]string{}
		counts := map[string]int64{}
		for _, a := range span.Attributes() {
			attrs[string(a.Key)] = a.Value.String()
			if string(a.Key) == "reopened" || string(a.Key) == "skipped_by_memo" {
				counts[string(a.Key)] = a.Value.AsInt64()
			}
		}
		if attrs["domain"] != wantDomains[i] || attrs["pass"] != "targeted" ||
			counts["reopened"] != wantReopened[i] || counts["skipped_by_memo"] != wantSkipped[i] {
			t.Errorf("span[%d] attrs=%v", i, attrs)
		}
	}
	lines := regexp.MustCompile(`deferred_backfill_block_completed block=reopen [^\n]* pass=targeted`).FindAllString(output.String(), -1)
	if len(lines) != len(wantDomains) {
		t.Fatalf("targeted reopen lines=%d, want %d: %s", len(lines), len(wantDomains), output.String())
	}
}
