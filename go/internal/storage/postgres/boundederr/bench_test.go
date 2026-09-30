// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"context"
	"database/sql"
	"testing"
)

// benchQuery runs one QueryContext that returns rowCount rows through db and
// drains it, the shape of every store read.
func benchQuery(b *testing.B, db *sql.DB) {
	b.Helper()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := db.QueryContext(ctx, "SELECT name FROM t WHERE id = $1", i)
		if err != nil {
			b.Fatal(err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				b.Fatal(err)
			}
		}
		if err := rows.Err(); err != nil {
			b.Fatal(err)
		}
		_ = rows.Close()
	}
}

// BenchmarkQueryBare is the pool over the fake driver with no wrapper: the
// baseline the wrapper's per-call cost is read against.
func BenchmarkQueryBare(b *testing.B) {
	db := sql.OpenDB(&fakeConnector{script: &script{rowsBefore: 20}})
	defer func() { _ = db.Close() }()
	benchQuery(b, db)
}

// BenchmarkQueryBounded is the same pool with the connector wrapped. A
// successful read pays the wrapper's forwarding calls and one rows wrapper
// allocation; it never builds an error or a log record.
func BenchmarkQueryBounded(b *testing.B) {
	db := sql.OpenDB(NewConnector(&fakeConnector{script: &script{rowsBefore: 20}}))
	defer func() { _ = db.Close() }()
	benchQuery(b, db)
}
