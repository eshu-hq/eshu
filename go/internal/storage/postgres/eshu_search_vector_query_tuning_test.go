// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

type searchVectorTuningDB struct {
	tx *searchVectorTuningTx
}

func (d *searchVectorTuningDB) Begin(context.Context) (Transaction, error) {
	return d.tx, nil
}

func (d *searchVectorTuningDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	panic("search vector query must run in the tuned transaction")
}

func (d *searchVectorTuningDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	panic("search vector tuning must run in the transaction")
}

type searchVectorTuningTx struct {
	execs     []string
	queries   []string
	commits   int
	rollbacks int
	commitErr error
}

func (t *searchVectorTuningTx) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	t.queries = append(t.queries, query)
	return &queueFakeRows{}, nil
}

func (t *searchVectorTuningTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	t.execs = append(t.execs, query)
	return zeroRowsResult{}, nil
}

func (t *searchVectorTuningTx) Commit() error {
	t.commits++
	return t.commitErr
}

func TestSearchVectorDocumentQueryRollsBackAfterCommitFailure(t *testing.T) {
	t.Parallel()

	tx := &searchVectorTuningTx{commitErr: errors.New("commit failed")}
	rows, err := beginSearchVectorDocumentQuery(context.Background(), &searchVectorTuningDB{tx: tx}, "SELECT 1")
	if err != nil {
		t.Fatalf("beginSearchVectorDocumentQuery error = %v", err)
	}
	if err := rows.Commit(); err == nil {
		t.Fatal("Commit error = nil, want transaction commit failure")
	}
	if tx.commits != 1 || tx.rollbacks != 1 {
		t.Fatalf("transaction completion = commits %d rollbacks %d, want 1/1", tx.commits, tx.rollbacks)
	}
}

func (t *searchVectorTuningTx) Rollback() error {
	t.rollbacks++
	return nil
}

func TestSearchVectorDocumentQueryDisablesJITLocally(t *testing.T) {
	t.Parallel()

	tx := &searchVectorTuningTx{}
	db := &searchVectorTuningDB{tx: tx}
	store := NewEshuSearchDocumentStore(db)
	_, err := store.ListPendingVectorDocumentsForScopes(context.Background(), EshuSearchVectorDocumentBatchFilter{
		Scopes:            []EshuSearchVectorDocumentScope{{ScopeID: "scope-a", GenerationID: "gen-a"}},
		ProviderProfileID: "local", SourceClass: "search_documents",
		EmbeddingModelID: "local-hash-v1", VectorIndexVersion: "vector-v1", Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListPendingVectorDocumentsForScopes error = %v", err)
	}
	if len(tx.execs) != 1 || tx.execs[0] != disableJITForSearchVectorDocumentQuerySQL {
		t.Fatalf("transaction execs = %#v, want SET LOCAL jit = off", tx.execs)
	}
	if len(tx.queries) != 1 {
		t.Fatalf("transaction queries = %d, want 1", len(tx.queries))
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("transaction completion = commits %d rollbacks %d, want 1/0", tx.commits, tx.rollbacks)
	}
}
