// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// openStandbyExportAccess opens an owned primary and physical standby, as a
// single reader pool or as a one-member fleet, with or without declaring the
// fixture build. It skips when the fixture is not configured.
func openStandbyExportAccess(t *testing.T, fleet, declare bool, observer Observer) *Access {
	t.Helper()
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if writer == "" || reader == "" {
		t.Skip("owned primary and physical standby not configured")
	}
	members := ""
	if fleet {
		endpoint, err := parsePhysicalEndpoint(reader)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal([]ReaderMember{{ID: "standby", Host: endpoint.Host, Port: endpoint.Port}})
		if err != nil {
			t.Fatal(err)
		}
		members = string(encoded)
	}
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return reader
		case "ESHU_POSTGRES_READ_MEMBERS":
			return members
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if declare {
		declareFixtureSnapshotExport(t, &cfg)
	}
	access, err := Open(context.Background(), cfg, observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := access.Close(); err != nil {
			t.Error(err)
		}
	})
	return access
}

// exportVerdictHolder returns the pool or sole member holder of access.
func exportVerdictHolder(access *Access) *snapshotExportQualification {
	if len(access.readerMembers) > 0 {
		return access.readerMembers[0].snapshotExport
	}
	return access.snapshotExport
}

// TestSnapshotExportRefusedOnUndeclaredStandby is the #7957 live denial on an
// owned physical standby: with no declaration, a fixture below PostgreSQL 18.7
// refuses the snapshot set with the sentinel, observes no reader borrow and no
// business query, counts one no_declaration denial, and holds no connection.
// Run it with log_statement=all on the standby to see that no
// pg_export_snapshot or SET TRANSACTION SNAPSHOT reaches the server.
func TestSnapshotExportRefusedOnUndeclaredStandby(t *testing.T) {
	for _, fleet := range []bool{false, true} {
		name := "single pool"
		if fleet {
			name = "fleet"
		}
		t.Run(name, func(t *testing.T) {
			observer := newExportSpyObserver()
			access := openStandbyExportAccess(t, fleet, false, observer)
			verdict := exportVerdictHolder(access).current()
			if verdict == nil {
				t.Fatal("Open did not record the standby build")
			}
			if verdict.qualified {
				t.Skipf("fixture build %q (%d) qualifies on the version floor; the denial is not reachable", verdict.serverVersion, verdict.serverVersionNum)
			}
			ctx, err := access.ContextWithCheckpoint(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			beforeBorrows, beforeBusiness, _ := observer.counts()
			set, err := access.Reader().(db.ReadSnapshotSetBeginner).BeginReadOnlySnapshotSet(ctx, 4)
			if set != nil || !errors.Is(err, db.ErrSnapshotExportUnqualified) {
				t.Fatalf("undeclared %q set = %v, err = %v", verdict.serverVersion, set, err)
			}
			borrows, business, denials := observer.counts()
			if borrows != beforeBorrows || business != beforeBusiness || !slices.Equal(denials, []string{snapshotExportReasonNoDeclaration}) {
				t.Fatalf("refusal observed borrows %d->%d business %d->%d denials %v", beforeBorrows, borrows, beforeBusiness, business, denials)
			}
			if _, stats := access.Stats(); stats.InUse != 0 {
				t.Fatalf("refusal holds %d reader connections", stats.InUse)
			}
			if fleet {
				assertAllocatorIdle(t, access)
			}
			var value int
			if err := access.Reader().QueryRowContext(ctx, "SELECT 1").Scan(&value); err != nil || value != 1 {
				t.Fatalf("plain read after refusal = %d, %v", value, err)
			}
		})
	}
}

// TestSnapshotExportDeclaredStandbySharesOneSnapshot is the positive control
// on the same fixture: declaring its exact server_version lets the single
// pool and the one-member fleet open a four-reader set whose readers all
// report one pg_current_snapshot().
func TestSnapshotExportDeclaredStandbySharesOneSnapshot(t *testing.T) {
	for _, fleet := range []bool{false, true} {
		name := "single pool"
		if fleet {
			name = "fleet"
		}
		t.Run(name, func(t *testing.T) {
			observer := newExportSpyObserver()
			access := openStandbyExportAccess(t, fleet, true, observer)
			if !exportVerdictHolder(access).qualified() {
				t.Fatalf("declared fixture verdict = %+v", exportVerdictHolder(access).current())
			}
			ctx, err := access.ContextWithCheckpoint(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			set, err := access.Reader().(db.ReadSnapshotSetBeginner).BeginReadOnlySnapshotSet(ctx, 4)
			if err != nil {
				t.Fatalf("declared set: %v", err)
			}
			snapshots := make([]string, 0, 4)
			for i := range 4 {
				queryer, err := set.Reader(i)
				if err != nil {
					t.Fatal(err)
				}
				rows, err := queryer.QueryContext(ctx, "SELECT pg_current_snapshot()::text")
				if err != nil {
					t.Fatal(err)
				}
				var snapshot string
				if !rows.Next() || rows.Scan(&snapshot) != nil {
					_ = rows.Close()
					t.Fatalf("reader %d snapshot row: %v", i, rows.Err())
				}
				if err := rows.Close(); err != nil {
					t.Fatal(err)
				}
				snapshots = append(snapshots, snapshot)
			}
			if err := set.Close(); err != nil {
				t.Fatal(err)
			}
			for i, snapshot := range snapshots {
				if snapshot != snapshots[0] {
					t.Fatalf("reader %d snapshot %q, want %q (all %v)", i, snapshot, snapshots[0], snapshots)
				}
			}
			if _, _, denials := observer.counts(); len(denials) != 0 {
				t.Fatalf("declared set counted denials %v", denials)
			}
			deadline := time.Now().Add(time.Second)
			for {
				if _, stats := access.Stats(); stats.InUse == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("declared set leaked reader connections")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}
