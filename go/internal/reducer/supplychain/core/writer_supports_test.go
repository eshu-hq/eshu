// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// supportsRecordingBeginner wraps the fake impact transaction with the supports
// maintenance surface the Postgres transaction implements, and records every
// statement shape and supports call in order, so a test can pin where the
// supports statements sit relative to the fact insert and the retraction
// (#10880 PR-B1: supports are written in the transaction that writes the facts).
type supportsRecordingBeginner struct {
	fakeImpactBeginner
	events    *[]string
	upsertErr error
	factIDs   *[][]string
}

func (b supportsRecordingBeginner) BeginSupplyChainImpactTx(ctx context.Context) (SupplyChainImpactTx, error) {
	tx, err := b.fakeImpactBeginner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return nil, err
	}
	return supportsRecordingTx{SupplyChainImpactTx: tx, events: b.events, upsertErr: b.upsertErr, factIDs: b.factIDs}, nil
}

type supportsRecordingTx struct {
	SupplyChainImpactTx
	events    *[]string
	upsertErr error
	factIDs   *[][]string
}

func (t supportsRecordingTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO fact_records"):
		*t.events = append(*t.events, "facts")
	case query == retractSupersededSupplyChainImpactFindingsQuery:
		*t.events = append(*t.events, "retract")
	}
	return t.SupplyChainImpactTx.ExecContext(ctx, query, args...)
}

func (t supportsRecordingTx) QueryContext(ctx context.Context, query string, args ...any) (SupplyChainImpactRows, error) {
	if query == retractSupersededSupplyChainImpactFindingsQuery {
		*t.events = append(*t.events, "retract")
	}
	return t.SupplyChainImpactTx.QueryContext(ctx, query, args...)
}

func (t supportsRecordingTx) UpsertImpactSupportRows(_ context.Context, rows []factwrite.VersionedRow) (SupplyChainImpactSupportsWrite, error) {
	*t.events = append(*t.events, "supports-upsert")
	factIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		factIDs = append(factIDs, row.FactID)
	}
	*t.factIDs = append(*t.factIDs, factIDs)
	if t.upsertErr != nil {
		return SupplyChainImpactSupportsWrite{}, t.upsertErr
	}
	return SupplyChainImpactSupportsWrite{Inserted: int64(len(rows))}, nil
}

func (t supportsRecordingTx) RecordRetractedImpactSupports(_ context.Context, rows int64) {
	*t.events = append(*t.events, fmt.Sprintf("supports-deleted:%d", rows))
}

func supportsWriter(upsertErr error) (PostgresSupplyChainImpactWriter, *[]string, *[][]string) {
	writer, beginner := changedTestWriter()
	events := &[]string{}
	factIDs := &[][]string{}
	writer.DB = supportsRecordingBeginner{fakeImpactBeginner: beginner, events: events, upsertErr: upsertErr, factIDs: factIDs}
	return writer, events, factIDs
}

// TestWriteMaintainsSupportsInTheFactTransaction pins the maintainer placement:
// the supports upsert runs after the fact insert and before the retraction, over
// exactly the rows the pass wrote, and the supports delete runs after the
// retraction for the pass's (scope, generation). A partial-evidence pass skips
// the retraction, so it skips the delete too.
func TestWriteMaintainsSupportsInTheFactTransaction(t *testing.T) {
	t.Parallel()

	first, second := changedTestFinding("CVE-2026-0001"), changedTestFinding("CVE-2026-0002")
	write := changedTestWrite(first, second)
	wantIDs := []string{supplyChainImpactFactID(write, first), supplyChainImpactFactID(write, second)}

	t.Run("full pass", func(t *testing.T) {
		t.Parallel()
		writer, events, factIDs := supportsWriter(nil)
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		want := []string{"facts", "supports-upsert", "retract", "supports-deleted:1"}
		if !reflect.DeepEqual(*events, want) {
			t.Fatalf("event order = %v, want %v", *events, want)
		}
		if len(*factIDs) != 1 || !reflect.DeepEqual((*factIDs)[0], wantIDs) {
			t.Fatalf("supports upsert fact ids = %v, want %v", *factIDs, wantIDs)
		}
	})

	t.Run("partial evidence skips the delete with the retraction", func(t *testing.T) {
		t.Parallel()
		partial := write
		partial.PartialEvidence = true
		writer, events, _ := supportsWriter(nil)
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), partial); err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
		}
		want := []string{"facts", "supports-upsert"}
		if !reflect.DeepEqual(*events, want) {
			t.Fatalf("event order = %v, want %v", *events, want)
		}
	})

	t.Run("a supports failure rolls the pass back", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("supports write failed")
		writer, _, _ := supportsWriter(boom)
		_, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
		if !errors.Is(err, boom) {
			t.Fatalf("error = %v, want it to wrap the supports failure", err)
		}
	})
}

// TestWriteSupplyChainImpactFindingRowsIsTheWritersOwnFactsAndSupportsStep pins
// the seam #11201 uses: the exported step writes the fact rows and then the
// supports of those fact ids in the caller's transaction, and is the very step
// the writer runs (a writer pass records the same facts-then-supports events).
func TestWriteSupplyChainImpactFindingRowsIsTheWritersOwnFactsAndSupportsStep(t *testing.T) {
	t.Parallel()

	writer, events, factIDs := supportsWriter(nil)
	tx, err := writer.DB.BeginSupplyChainImpactTx(context.Background())
	if err != nil {
		t.Fatalf("BeginSupplyChainImpactTx() error = %v, want nil", err)
	}
	rows := []factwrite.VersionedRow{
		{FactID: "fact-1", ScopeID: "s", GenerationID: "g", FactKind: supplyChainImpactFactKind},
		{FactID: "fact-2", ScopeID: "s", GenerationID: "g", FactKind: supplyChainImpactFactKind},
	}
	if err := WriteSupplyChainImpactFindingRows(context.Background(), tx, rows, nil); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindingRows() error = %v, want nil", err)
	}
	if want := []string{"facts", "supports-upsert"}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
	if len(*factIDs) != 1 || !reflect.DeepEqual((*factIDs)[0], []string{"fact-1", "fact-2"}) {
		t.Fatalf("supports upsert fact ids = %v, want fact-1 and fact-2", *factIDs)
	}

	// No rows: nothing is written, supports included.
	*events = nil
	if err := WriteSupplyChainImpactFindingRows(context.Background(), tx, nil, nil); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindingRows(nil) error = %v, want nil", err)
	}
	if len(*events) != 0 {
		t.Fatalf("events for no rows = %v, want none", *events)
	}
}
