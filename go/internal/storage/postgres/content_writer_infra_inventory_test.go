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

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
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

	if got, want := len(database.txExecs), 3; got != want {
		t.Fatalf("derive statements = %d, want lock+delete+insert", got)
	}
	if database.commits != 1 || database.rollbacks != 0 {
		t.Fatalf("commits=%d rollbacks=%d, want one committed derive", database.commits, database.rollbacks)
	}
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
