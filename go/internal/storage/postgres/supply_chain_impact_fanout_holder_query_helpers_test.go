// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
)

// holderCoveringCTEHead is the byte-prefix of the holder statement's MATERIALIZED
// candidate set: the proofs that split the statement by parameter restriction pin
// it so a change to the query shape fails them instead of skewing one side.
const holderCoveringCTEHead = "WITH candidates AS MATERIALIZED ("

// holderQueryRunner is the QueryContext surface shared by *sql.DB and
// *sql.Tx.
type holderQueryRunner interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// queryScopes runs a two-argument (ids, ownScope) holder query and returns
// the ordered scope ids.
func queryScopes(t *testing.T, ctx context.Context, querier holderQueryRunner, query string, ids []string, ownScope string) []string {
	t.Helper()
	rows, err := querier.QueryContext(ctx, query, ids, ownScope)
	if err != nil {
		t.Fatalf("holder query %v: %v", ids, err)
	}
	var out []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		out = append(out, scope)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read holder rows: %v", err)
	}
	return out
}
