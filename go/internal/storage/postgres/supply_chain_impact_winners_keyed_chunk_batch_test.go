// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// chunkBatchFakeRows is a canned db.Rows over an identifier list.
type chunkBatchFakeRows struct {
	idents [][5]string
	pos    int
}

func (r *chunkBatchFakeRows) Next() bool { return r.pos < len(r.idents) }

func (r *chunkBatchFakeRows) Scan(dest ...any) error {
	if r.pos >= len(r.idents) {
		return fmt.Errorf("scan past end of chunk batch fake rows")
	}
	if len(dest) != 5 {
		return fmt.Errorf("chunk batch fake rows scan wants 5 destinations, got %d", len(dest))
	}
	for i := 0; i < 5; i++ {
		ptr, ok := dest[i].(*string)
		if !ok {
			return fmt.Errorf("chunk batch fake rows scan destination %d is not *string", i)
		}
		*ptr = r.idents[r.pos][i]
	}
	r.pos++
	return nil
}

func (r *chunkBatchFakeRows) Err() error { return nil }

func (r *chunkBatchFakeRows) Close() error { return nil }

// chunkBatchFakeTx records every Exec and serves one canned identifier list
// for the chunk's SELECT. It proves the tick's statement shape without a
// database: the capacity lane (#10839) bounds on per-tick round trips.
type chunkBatchFakeTx struct {
	idents [][5]string
	execs  []string
	args   [][]any
}

func (f *chunkBatchFakeTx) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return &chunkBatchFakeRows{idents: f.idents}, nil
}

func (f *chunkBatchFakeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, query)
	f.args = append(f.args, args)
	return fakeChunkBatchResult{}, nil
}

func (f *chunkBatchFakeTx) Commit() error   { return nil }
func (f *chunkBatchFakeTx) Rollback() error { return nil }

type fakeChunkBatchResult struct{}

func (fakeChunkBatchResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeChunkBatchResult) RowsAffected() (int64, error) { return 0, nil }

// chunkBatchIdents builds n distinct canned identifiers.
func chunkBatchIdents(n int) [][5]string {
	out := make([][5]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, [5]string{
			fmt.Sprintf("pkg-%d", i),
			fmt.Sprintf("repo-%d", i),
			fmt.Sprintf("digest-%d", i),
			fmt.Sprintf("c0-%d", i),
			fmt.Sprintf("adv-%d", i),
		})
	}
	return out
}

// insertExecs counts the Exec calls that insert chunk identifiers.
func insertExecs(f *chunkBatchFakeTx) int {
	n := 0
	for _, q := range f.execs {
		trimmed := strings.TrimSpace(q)
		if strings.HasPrefix(strings.ToUpper(trimmed), "INSERT") && strings.Contains(q, "winners_keyed_chunk_idents") {
			n++
		}
	}
	return n
}

// TestBuildWinnersKeyedChunkBatchesIdentifierInserts is the #10839 theory
// shim in test form: a saturated 200-identifier tick must pack the whole
// chunk into one INSERT statement, not one round trip per identifier. The
// per-tick round-trip count is the capacity term this lane removes; the
// chunk bound, order, and truncate rule are unchanged.
func TestBuildWinnersKeyedChunkBatchesIdentifierInserts(t *testing.T) {
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(201)}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 5000, identifiers: 200})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk: %v", err)
	}
	if outcome.identifiers != 200 {
		t.Fatalf("identifiers = %d, want 200", outcome.identifiers)
	}
	if !outcome.truncated {
		t.Fatalf("truncated = false, want true for a 201-row identifier set against a 200 bound")
	}
	if got := insertExecs(fake); got != 1 {
		t.Fatalf("identifier INSERT statements = %d, want 1 batched statement", got)
	}
	// The batch carries every admitted identifier in oldest-first order.
	var gotIDs [][5]string
	for _, argSet := range fake.args {
		for i := 0; i+5 <= len(argSet); i += 5 {
			var id [5]string
			for j := 0; j < 5; j++ {
				s, ok := argSet[i+j].(string)
				if !ok {
					t.Fatalf("batched arg %d is %T, want string", i+j, argSet[i+j])
				}
				id[j] = s
			}
			gotIDs = append(gotIDs, id)
		}
	}
	if len(gotIDs) != 200 {
		t.Fatalf("batched identifiers = %d, want 200", len(gotIDs))
	}
	for i := range gotIDs {
		if gotIDs[i] != fake.idents[i] {
			t.Fatalf("batched identifier %d = %v, want %v (oldest-first order)", i, gotIDs[i], fake.idents[i])
		}
	}
}

// TestBuildWinnersKeyedChunkSingleIdentifierCompletes keeps the hot-partition
// rule: one identifier still drains in one INSERT, never zero.
func TestBuildWinnersKeyedChunkSingleIdentifierCompletes(t *testing.T) {
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(1)}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 5000, identifiers: 1})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk: %v", err)
	}
	if outcome.identifiers != 1 || outcome.truncated {
		t.Fatalf("outcome = %+v, want {identifiers:1 truncated:false}", outcome)
	}
	if got := insertExecs(fake); got != 1 {
		t.Fatalf("identifier INSERT statements = %d, want 1", got)
	}
}

// TestBuildWinnersKeyedChunkEmptyInsertsNothing keeps the empty-window rule:
// no identifier INSERT runs when the set is empty.
func TestBuildWinnersKeyedChunkEmptyInsertsNothing(t *testing.T) {
	fake := &chunkBatchFakeTx{}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 5000, identifiers: 200})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk: %v", err)
	}
	if outcome.identifiers != 0 || outcome.truncated {
		t.Fatalf("outcome = %+v, want {identifiers:0 truncated:false}", outcome)
	}
	if got := insertExecs(fake); got != 0 {
		t.Fatalf("identifier INSERT statements = %d, want 0", got)
	}
}
