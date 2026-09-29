// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codetopicparallel

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// connectionGate serializes only partial reservations against one sql.DB.
// References keep the gate in the registry until its last waiter exits.
type connectionGate struct {
	permit chan struct{}
	refs   int
}

var connectionGates = struct {
	sync.Mutex
	byDB map[*sql.DB]*connectionGate
}{byDB: make(map[*sql.DB]*connectionGate)}

func gateForDB(db *sql.DB) (*connectionGate, func()) {
	connectionGates.Lock()
	gate := connectionGates.byDB[db]
	if gate == nil {
		gate = &connectionGate{permit: make(chan struct{}, 1)}
		gate.permit <- struct{}{}
		connectionGates.byDB[db] = gate
	}
	gate.refs++
	connectionGates.Unlock()
	return gate, func() {
		connectionGates.Lock()
		gate.refs--
		if gate.refs == 0 {
			delete(connectionGates.byDB, db)
		}
		connectionGates.Unlock()
	}
}

func closeConnections(conns []*sql.Conn) {
	for i := len(conns) - 1; i >= 0; i-- {
		_ = conns[i].Close()
	}
}

// reserveConnections prevents competing requests from each holding a partial
// set of connections while waiting for the same bounded pool. Independent DB
// handles use independent gates; callers release all conns after transactions.
func reserveConnections(ctx context.Context, db *sql.DB, count int) ([]*sql.Conn, error) {
	gate, releaseRef := gateForDB(db)
	defer releaseRef()
	select {
	case <-gate.permit:
		defer func() { gate.permit <- struct{}{} }()
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for code topic reservation: %w", ctx.Err())
	}
	conns := make([]*sql.Conn, 0, count)
	for range count {
		conn, err := db.Conn(ctx)
		if err != nil {
			closeConnections(conns)
			return nil, fmt.Errorf("reserve code topic connection: %w", err)
		}
		conns = append(conns, conn)
	}
	return conns, nil
}
