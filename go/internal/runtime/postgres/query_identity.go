// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"net"

	"github.com/jackc/pgx/v5/stdlib"
)

// readerBackendIdentity contains only values copied from the exact leased
// physical reader connection. The driver connection never escapes Raw.
type readerBackendIdentity struct {
	pid       uint32
	remote    string
	available bool
}

type readerQueryStartObserver interface {
	recordsReaderQueryStart(context.Context) bool
	recordReaderQueryStart(context.Context, int64, readerBackendIdentity)
}

// captureReaderBackendIdentity does not change connection health when the
// driver does not expose a pgx connection or its remote TCP endpoint.
func captureReaderBackendIdentity(conn *sql.Conn) readerBackendIdentity {
	if conn == nil {
		return readerBackendIdentity{}
	}
	var identity readerBackendIdentity
	_ = conn.Raw(func(raw any) error {
		pgxConn, ok := raw.(*stdlib.Conn)
		if !ok || pgxConn.Conn() == nil || pgxConn.Conn().PgConn() == nil {
			return nil
		}
		backend := pgxConn.Conn().PgConn()
		network := backend.Conn()
		if network == nil {
			return nil
		}
		remote, ok := network.RemoteAddr().(*net.TCPAddr)
		if !ok || remote.IP == nil || remote.Port == 0 || backend.PID() == 0 {
			return nil
		}
		identity = readerBackendIdentity{pid: backend.PID(), remote: remote.String(), available: true}
		return nil
	})
	return identity
}

func (a *Access) readerQueryStartObserver(ctx context.Context) readerQueryStartObserver {
	if a == nil {
		return nil
	}
	observer, ok := a.observer.(readerQueryStartObserver)
	if !ok || !observer.recordsReaderQueryStart(ctx) {
		return nil
	}
	return observer
}

func (a *Access) startReaderQuery(ctx context.Context, conn *sql.Conn) {
	observer := a.readerQueryStartObserver(ctx)
	if observer == nil {
		return
	}
	observer.recordReaderQueryStart(ctx, a.querySequence.Add(1), captureReaderBackendIdentity(conn))
}

func (a *Access) recordReaderQueryStart(ctx context.Context, identity readerBackendIdentity) {
	observer := a.readerQueryStartObserver(ctx)
	if observer == nil {
		return
	}
	observer.recordReaderQueryStart(ctx, a.querySequence.Add(1), identity)
}
