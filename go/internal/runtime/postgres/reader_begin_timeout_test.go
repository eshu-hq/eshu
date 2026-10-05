// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestFleetSetupTimeoutPreservesFatalSiblingAndUnexpectedCleanup(t *testing.T) {
	deadline := snapshotSetupTimeout{cause: errors.Join(context.Canceled, sql.ErrTxDone)}
	if sharedFleetFailure(context.Background(), deadline) {
		t.Fatal("proved setup deadline was classified fatal")
	}
	joined := errors.Join(deadline, &pgconn.PgError{Code: "28P01"})
	if !sharedFleetFailure(context.Background(), joined) {
		t.Fatal("authentication failure was masked by setup deadline")
	}
	if expectedCanceledSetupCleanup(errors.Join(sql.ErrConnDone, errors.New("unknown cleanup failure"))) {
		t.Fatal("unexpected cleanup failure was accepted as a canceled transaction")
	}
}

type stalledSetupConn struct {
	net.Conn
	statement []byte
	stageSeen chan<- struct{}
	closed    *atomic.Int64
	once      sync.Once
}

func (c *stalledSetupConn) Write(data []byte) (int, error) {
	if bytes.Contains(bytes.ToUpper(data), c.statement) {
		select {
		case c.stageSeen <- struct{}{}:
		default:
		}
		// The server cannot answer this setup statement before cancellation.
		return len(data), nil
	}
	return c.Conn.Write(data)
}

func (c *stalledSetupConn) Close() error {
	c.once.Do(func() { c.closed.Add(1) })
	return c.Conn.Close()
}

func TestFleetBeginHonorsSetupDeadlineBeforeRequestDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	beginSeen := make(chan struct{}, 1)
	serverDone := make(chan struct{})
	var workers sync.WaitGroup
	go func() {
		defer close(serverDone)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				serveCleanupShim(conn, beginSeen)
			}()
		}
	}()
	tracked := &bootstrapDialTracker{}
	config, err := pgx.ParseConfig("postgres://fixture:fixture@127.0.0.1:5432/eshu?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	baseDial := (&net.Dialer{}).DialContext
	config.DialFunc = tracked.wrap(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return baseDial(ctx, network, listener.Addr().String())
	})
	pool := stdlib.OpenDB(*config)
	defer pool.Close()
	ownerCtx, cancelOwner := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancelOwner()
	setupCtx, cancelSetup := context.WithTimeout(ownerCtx, 100*time.Millisecond)
	defer cancelSetup()
	txCtx, cancelTx, detach := bridgeSnapshotSetup(setupCtx, ownerCtx)
	defer func() {
		cancelTx(context.Canceled)
		detach()
	}()
	conn, err := pool.Conn(ownerCtx)
	if err != nil {
		t.Fatalf("connect shim: %v", err)
	}
	defer conn.Close()
	started := time.Now()
	_, err = beginReadTransactionOwned(txCtx, &readerConnection{Conn: conn}, &Access{})
	elapsed := time.Since(started)
	select {
	case <-beginSeen:
	default:
		t.Fatal("shim did not receive BEGIN")
	}
	if err == nil {
		t.Fatal("stalled BEGIN unexpectedly succeeded")
	}
	_ = tracked.closeAll()
	_ = listener.Close()
	<-serverDone
	workers.Wait()
	if elapsed > 250*time.Millisecond {
		t.Fatalf("stalled BEGIN took %s past 100ms setup budget; request deadline was 600ms", elapsed)
	}
	if ownerCtx.Err() != nil {
		t.Fatalf("setup failure consumed request deadline: %v", ownerCtx.Err())
	}
}

func TestFleetSnapshotSetupTimeoutRetriesHealthyMemberAndKeepsSetAlive(t *testing.T) {
	if os.Getenv("ESHU_READER_TEST_SECOND_READER_DSN") == "" {
		t.Skip("owned primary and two physical standbys not configured")
	}
	for _, tc := range []struct {
		name      string
		statement []byte
	}{
		{"begin", []byte("BEGIN")},
		{"export", []byte("PG_EXPORT_SNAPSHOT")},
		{"import", []byte("SET TRANSACTION SNAPSHOT")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testFleetSnapshotSetupTimeoutRetry(t, tc.statement)
		})
	}
}

func TestFleetReturnedSnapshotSetReleasesOnCallerCancellation(t *testing.T) {
	if os.Getenv("ESHU_READER_TEST_SECOND_READER_DSN") == "" {
		t.Skip("owned primary and two physical standbys not configured")
	}
	access := openFleetRegressionAccess(t, 500*time.Millisecond)
	ownerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, err := access.ContextWithCheckpoint(ownerCtx)
	if err != nil {
		t.Fatal(err)
	}
	set, err := access.Reader().(db.ReadSnapshotSetBeginner).BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		_, stats := access.Stats()
		if stats.InUse == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("caller cancellation left %d readers borrowed", stats.InUse)
		}
		time.Sleep(time.Millisecond)
	}
	// Cancellation may win database/sql's rollback; the pool must still drain.
	_ = set.Close()
}

func testFleetSnapshotSetupTimeoutRetry(t *testing.T, statement []byte) {
	t.Helper()
	access := openFleetRegressionAccess(t, 500*time.Millisecond)
	config, err := parsePhysicalEndpoint(os.Getenv("ESHU_READER_TEST_READER_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["default_transaction_read_only"] = "on"
	member := &access.readerMembers[0]
	config.ValidateConnect = memberValidator(access.lineage.identity().physicalIdentity, member.incarnation, member.addresses)
	baseDial := config.DialFunc
	if baseDial == nil {
		baseDial = (&net.Dialer{}).DialContext
	}
	stageSeen := make(chan struct{}, 1)
	var opened, closed atomic.Int64
	config.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, dialErr := baseDial(ctx, network, address)
		if dialErr != nil {
			return nil, dialErr
		}
		opened.Add(1)
		return &stalledSetupConn{Conn: conn, statement: statement, stageSeen: stageSeen, closed: &closed}, nil
	}
	if err := member.pool.Close(); err != nil {
		t.Fatal(err)
	}
	member.pool = stdlib.OpenDB(*config)
	member.pool.SetMaxOpenConns(member.maxOpen)
	member.pool.SetMaxIdleConns(0)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	access.nextReader.Store(0)
	started := time.Now()
	set, err := access.Reader().(db.ReadSnapshotSetBeginner).BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatalf("setup timeout blocked healthy peer: %v", err)
	}
	defer set.Close()
	if time.Since(started) < 80*time.Millisecond {
		t.Fatalf("stalled setup returned before its timeout: %s", time.Since(started))
	}
	if time.Since(started) >= 450*time.Millisecond {
		t.Fatalf("healthy peer missed replay budget after stalled setup: %s", time.Since(started))
	}
	select {
	case <-stageSeen:
	default:
		t.Fatalf("first member did not reach stalled setup: opened=%d closed=%d selected0=%+v selected1=%+v", opened.Load(), closed.Load(), access.readerMembers[0].pool.Stats(), access.readerMembers[1].pool.Stats())
	}
	if got := access.readerMembers[1].pool.Stats().InUse; got != 4 {
		t.Fatalf("healthy peer holds %d readers, want 4", got)
	}
	time.Sleep(125 * time.Millisecond)
	reader, err := set.Reader(0)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := reader.QueryContext(ctx, "SELECT pg_is_in_recovery()")
	if err != nil {
		t.Fatal(err)
	}
	var recovery bool
	if !rows.Next() {
		t.Fatalf("returned snapshot died after setup timer: %v", rows.Err())
	}
	if err := rows.Scan(&recovery); err != nil || !recovery {
		t.Fatalf("returned snapshot returned invalid recovery: recovery=%t err=%v", recovery, err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	if _, stats := access.Stats(); stats.InUse != 0 {
		t.Fatalf("failed setup left borrowed connections: %+v", stats)
	}
	if err := access.Close(); err != nil {
		t.Fatal(err)
	}
	if opened.Load() == 0 || opened.Load() != closed.Load() {
		t.Fatalf("stalled member sockets opened=%d closed=%d", opened.Load(), closed.Load())
	}
}
