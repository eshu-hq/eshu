// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5"
)

// TestSnapshotExportConsistencySmoke is the #7957 AC2(c) opt-in smoke on a
// qualified reader build. It seeds a small table on the owned primary, waits
// for the standby to replay it, opens a four-reader shared snapshot on the
// standby, and requires every imported reader and a fifth, plain standby
// session to read identical rows. It only reads through an imported snapshot;
// it creates no subtransaction and runs no VACUUM, so it does not attempt to
// reproduce either upstream defect. It runs only with
// ESHU_READER_TEST_SNAPSHOT_EXPORT_SMOKE=1 and the owned
// ESHU_READER_TEST_WRITER_DSN and ESHU_READER_TEST_READER_DSN, takes any build
// declaration from ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS, and skips on an
// unqualified build. It hashes with sha256, not md5: a server image that sets
// OPENSSL_FIPS=yes refuses md5() and would fail the smoke before any export. It
// is a plain env-gated test outside the live-test ledger
// and stays out of CI until the CI image is qualified.
func TestSnapshotExportConsistencySmoke(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if os.Getenv("ESHU_READER_TEST_SNAPSHOT_EXPORT_SMOKE") != "1" || writer == "" || reader == "" {
		t.Skip("snapshot-export consistency smoke requires the explicit opt-in and an owned primary and standby")
	}
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return reader
		case snapshotExportBuildsEnv:
			return os.Getenv(snapshotExportBuildsEnv)
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = access.Close() })
	if verdict := access.snapshotExport.current(); verdict == nil || !verdict.qualified {
		t.Skipf("reader build is not qualified for snapshot export: %+v", verdict)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	table := fmt.Sprintf("eshu_snapshot_export_smoke_%d", time.Now().UnixNano())
	primary, err := pgx.Connect(ctx, writer)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = primary.Close(context.Background()) }()
	if _, err := primary.Exec(ctx, "CREATE TABLE "+pgx.Identifier{table}.Sanitize()+" AS SELECT g AS id, encode(sha256(convert_to(g::text, 'UTF8')), 'hex') AS body FROM generate_series(1, 5000) AS g"); err != nil {
		t.Fatal(err)
	}
	// Deferred after the Close defer, so it runs first while primary is open.
	defer func() {
		if _, err := primary.Exec(context.Background(), "DROP TABLE IF EXISTS "+pgx.Identifier{table}.Sanitize()); err != nil {
			t.Errorf("drop smoke table: %v", err)
		}
	}()

	checked, err := access.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	set, err := access.Reader().(db.ReadSnapshotSetBeginner).BeginReadOnlySnapshotSet(checked, 4)
	if err != nil {
		t.Fatalf("qualified set: %v", err)
	}
	defer func() { _ = set.Close() }()
	digest := "SELECT count(*)::text || ':' || encode(sha256(convert_to(string_agg(id::text || body, ',' ORDER BY id), 'UTF8')), 'hex') FROM " + pgx.Identifier{table}.Sanitize()
	readings := make([]string, 0, 5)
	for i := range 4 {
		queryer, err := set.Reader(i)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := queryer.QueryContext(checked, digest)
		if err != nil {
			t.Fatal(err)
		}
		var reading string
		if !rows.Next() || rows.Scan(&reading) != nil {
			_ = rows.Close()
			t.Fatalf("reader %d digest: %v", i, rows.Err())
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		readings = append(readings, reading)
	}
	plain, err := pgx.Connect(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = plain.Close(context.Background()) }()
	var plainReading string
	if err := plain.QueryRow(ctx, digest).Scan(&plainReading); err != nil {
		t.Fatal(err)
	}
	readings = append(readings, plainReading)
	for i, reading := range readings {
		if reading != readings[0] {
			t.Fatalf("session %d read %q, want %q (all %v)", i, reading, readings[0], readings)
		}
	}
	if readings[0][:5] != "5000:" {
		t.Fatalf("snapshot did not see the replayed seed: %q", readings[0])
	}
}
