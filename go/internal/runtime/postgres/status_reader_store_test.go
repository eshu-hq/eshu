// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	pgstore "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type emptyStatusRows struct{}

func (emptyStatusRows) Next() bool        { return false }
func (emptyStatusRows) Scan(...any) error { return errors.New("unexpected scan") }
func (emptyStatusRows) Err() error        { return nil }
func (emptyStatusRows) Close() error      { return nil }

type emptyStatusTx struct {
	*snapshotTxStub
	queries   int
	faultAt   int
	faultRows db.Rows
	faultErr  error
}

func (s *emptyStatusTx) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	s.queries++
	if s.commits != 0 {
		return nil, errors.New("query after commit")
	}
	if s.queries == s.faultAt {
		if s.faultErr != nil {
			return nil, s.faultErr
		}
		return s.faultRows, nil
	}
	return emptyStatusRows{}, nil
}

func TestSnapshotStatusReaderRunsRealEmptyStatusStoreOnOneTransaction(t *testing.T) {
	tx := &emptyStatusTx{snapshotTxStub: &snapshotTxStub{}}
	store := snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }}
	reader := NewSnapshotStatusReader(store, func(q db.Queryer) status.Reader {
		return pgstore.NewStatusStore(q)
	}, nil)
	asOf := time.Now()
	raw, err := reader.ReadStatusSnapshotFiltered(t.Context(), asOf, status.SnapshotSelection{})
	if err != nil {
		t.Fatalf("empty filtered status: %v", err)
	}
	if !raw.AsOf.Equal(asOf) || tx.queries < 5 || tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("raw.AsOf=%v queries=%d commit=%d rollback=%d", raw.AsOf, tx.queries, tx.commits, tx.rollbacks)
	}
}

type oneBadStatusRow struct {
	yielded bool
	scanErr error
	decode  bool
}

func (r *oneBadStatusRow) Next() bool {
	if r.yielded {
		return false
	}
	r.yielded = true
	return true
}

func (r *oneBadStatusRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.decode {
		*dest[0].(*string) = "queue"
		*dest[1].(*int64) = 1
		*dest[2].(*string) = "not-json"
	}
	return nil
}
func (*oneBadStatusRow) Err() error   { return nil }
func (*oneBadStatusRow) Close() error { return nil }

func TestSnapshotStatusReaderRealStoreQueryScanDecodeErrorsRollback(t *testing.T) {
	queryErr := errors.New("query failed")
	scanErr := errors.New("scan failed")
	for _, tc := range []struct {
		name           string
		at             int
		rows           db.Rows
		queryErr, want error
	}{
		{"query", 1, nil, queryErr, queryErr},
		{"scan", 1, &oneBadStatusRow{scanErr: scanErr}, nil, scanErr},
		{"decode", 4, &oneBadStatusRow{decode: true}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &emptyStatusTx{snapshotTxStub: &snapshotTxStub{}, faultAt: tc.at, faultRows: tc.rows, faultErr: tc.queryErr}
			reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
				func(q db.Queryer) status.Reader { return pgstore.NewStatusStore(q) }, nil)
			raw, err := reader.ReadStatusSnapshotFiltered(t.Context(), time.Now(), status.SnapshotSelection{})
			if !raw.AsOf.IsZero() || err == nil || tx.queries != tc.at || tx.commits != 0 || tx.rollbacks != 1 {
				t.Fatalf("raw=%v error=%v queries=%d commit=%d rollback=%d", raw.AsOf, err, tx.queries, tx.commits, tx.rollbacks)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
}
