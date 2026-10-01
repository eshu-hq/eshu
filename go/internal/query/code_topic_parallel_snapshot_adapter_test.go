// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// codeTopicTestSnapshotStore supplies the SQL-driver tests with the optional
// snapshot-set contract. Production uses runtime/postgres's fenced reader.
type codeTopicTestSnapshotStore struct {
	db.ReadStore
	handle *sql.DB
}

var codeTopicTestGates sync.Map

func newCodeTopicParallelTestReader(handle *sql.DB) *ContentReader {
	return NewContentReaderWithReadStore(codeTopicTestSnapshotStore{
		ReadStore: postgres.NewSQLReadStore(handle), handle: handle,
	})
}

func (store codeTopicTestSnapshotStore) MaxReadConnections() int {
	return store.handle.Stats().MaxOpenConnections
}

type codeTopicTestSnapshotSet struct {
	conns []*sql.Conn
	txs   []*sql.Tx
}

func (set *codeTopicTestSnapshotSet) Reader(index int) (db.Queryer, error) {
	if index < 0 || index >= len(set.txs) || set.txs[index] == nil {
		return nil, fmt.Errorf("invalid test snapshot reader %d", index)
	}
	return postgres.SQLTx{Tx: set.txs[index]}, nil
}

func (set *codeTopicTestSnapshotSet) Close() error {
	var first error
	for i := len(set.txs) - 1; i >= 0; i-- {
		if set.txs[i] != nil {
			if err := set.txs[i].Rollback(); err != nil && err != sql.ErrTxDone && first == nil {
				first = err
			}
		}
	}
	for i := len(set.conns) - 1; i >= 0; i-- {
		if err := set.conns[i].Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (store codeTopicTestSnapshotStore) BeginReadOnlySnapshotSet(ctx context.Context, count int) (db.ReadSnapshotSet, error) {
	gateAny, _ := codeTopicTestGates.LoadOrStore(store.handle, make(chan struct{}, 1))
	gate := gateAny.(chan struct{})
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	set := &codeTopicTestSnapshotSet{}
	var reserveErr error
	for range count {
		conn, err := store.handle.Conn(ctx)
		if err != nil {
			reserveErr = err
			break
		}
		set.conns = append(set.conns, conn)
	}
	<-gate
	if reserveErr != nil {
		_ = set.Close()
		return nil, reserveErr
	}
	options := &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	exporter, err := set.conns[0].BeginTx(ctx, options)
	if err != nil {
		_ = set.Close()
		return nil, err
	}
	set.txs = append(set.txs, exporter)
	var snapshot string
	if err := exporter.QueryRowContext(ctx, "SELECT pg_export_snapshot()").Scan(&snapshot); err != nil {
		_ = set.Close()
		return nil, err
	}
	for _, conn := range set.conns[1:] {
		tx, err := conn.BeginTx(ctx, options)
		if err != nil {
			_ = set.Close()
			return nil, err
		}
		set.txs = append(set.txs, tx)
		if _, err := tx.ExecContext(ctx, "SET TRANSACTION SNAPSHOT '"+snapshot+"'"); err != nil {
			_ = set.Close()
			return nil, err
		}
	}
	return set, nil
}
