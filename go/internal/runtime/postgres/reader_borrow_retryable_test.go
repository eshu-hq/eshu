// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"
)

// idleConnector hands out connections that never fail, so the only way a
// borrow fails is by waiting on a saturated pool.
type idleConnector struct{}

type idleConn struct{}

func (idleConnector) Connect(context.Context) (driver.Conn, error) { return idleConn{}, nil }
func (idleConnector) Driver() driver.Driver                        { return terminalErrorDriver{} }

func (idleConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (idleConn) Close() error                        { return nil }
func (idleConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }

// TestReaderBorrowPoolWaitTimeoutIsRetryableDeadline pins the #7523 review: the
// query layer answers a retryable 503 only when a reader failure is
// ErrReaderUnavailable AND context.DeadlineExceeded. A real saturated reader
// pool must produce exactly that through the private-failure wrapper, while a
// connect failure must not carry a deadline.
func TestReaderBorrowPoolWaitTimeoutIsRetryableDeadline(t *testing.T) {
	pool := sql.OpenDB(idleConnector{})
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	held, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatalf("hold the only pooled connection: %v", err)
	}
	defer held.Close()

	access := &Access{writer: pool, reader: pool, replayTimeout: 30 * time.Millisecond}
	ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{owner: access})

	started := time.Now()
	_, err = access.borrowFresh(ctx)
	if err == nil {
		t.Fatal("borrow from a saturated pool succeeded")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("pool wait ignored the replay timeout: %s", time.Since(started))
	}
	if !errors.Is(err, ErrReaderUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool-wait timeout = %v, want ErrReaderUnavailable and context.DeadlineExceeded", err)
	}
	if strings.Contains(err.Error(), "deadline") {
		t.Fatalf("pool-wait error text exposes Go detail: %q", err.Error())
	}

	refused := sql.OpenDB(metadataConnector{cause: errors.New("dial tcp 10.0.0.9:5432: connect: connection refused")})
	defer refused.Close()
	failing := &Access{writer: refused, reader: refused, replayTimeout: time.Second}
	failCtx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{owner: failing})
	_, err = failing.borrowFresh(failCtx)
	if !errors.Is(err, ErrReaderUnavailable) {
		t.Fatalf("connect failure lost ErrReaderUnavailable: %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("connect failure %v must not look like a pool-wait deadline", err)
	}
}
