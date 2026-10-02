// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestReadSnapshotSetSharesOneSnapshot(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned physical reader fixture not configured")
	}
	access := testAccess(t, reader)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	setBuilder, ok := access.Reader().(db.ReadSnapshotSetBeginner)
	if !ok {
		t.Fatal("guarded reader does not expose snapshot sets")
	}
	for _, count := range []int{4, 8} {
		if count > access.reader.Stats().MaxOpenConnections {
			continue
		}
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			set, err := setBuilder.BeginReadOnlySnapshotSet(ctx, count)
			if err != nil {
				t.Fatal(err)
			}
			defer set.Close()
			if got := access.reader.Stats().InUse; got != count {
				t.Fatalf("reserved readers=%d, want %d", got, count)
			}
			if _, err := access.ContextWithCheckpoint(ctx); err != nil {
				t.Fatalf("writer pool checkpoint while reader pool is reserved: %v", err)
			}
			var snapshot string
			for i := 0; i < count; i++ {
				queryer, err := set.Reader(i)
				if err != nil {
					t.Fatalf("reader %d: %v", i, err)
				}
				rows, err := queryer.QueryContext(ctx, "SELECT pg_current_snapshot()::text")
				if err != nil {
					t.Fatalf("reader %d snapshot query: %v", i, err)
				}
				if !rows.Next() {
					_ = rows.Close()
					t.Fatalf("reader %d snapshot row: %v", i, rows.Err())
				}
				var got string
				if err := rows.Scan(&got); err != nil {
					_ = rows.Close()
					t.Fatalf("reader %d snapshot: %v", i, err)
				}
				if err := rows.Close(); err != nil {
					t.Fatalf("reader %d close: %v", i, err)
				}
				if i == 0 {
					snapshot = got
				} else if got != snapshot {
					t.Fatalf("reader %d snapshot=%q, want %q", i, got, snapshot)
				}
			}
			if err := set.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("snapshot set leaked %d readers", got)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	set, err := setBuilder.BeginReadOnlySnapshotSet(cancelCtx, 4)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for access.reader.Stats().InUse != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("canceled active set leaked %d readers", got)
	}
	// database/sql may race the cancellation callback's Rollback. The first
	// closer can report context cancellation or ErrTxDone even though every
	// physical reader was released; unrelated terminal errors still fail.
	closeErr := set.Close()
	for i, reader := range set.(*readSnapshotSet).readers {
		if reader.err == nil {
			continue
		}
		var terminal privateError
		if !errors.As(reader.err, &terminal) ||
			(!errors.Is(terminal.cause, context.Canceled) && !errors.Is(terminal.cause, sql.ErrTxDone)) {
			t.Fatalf("reader %d canceled terminal cause: %v", i, reader.err)
		}
	}
	if again := set.Close(); again != closeErr {
		t.Fatalf("repeated close error = %v, want %v", again, closeErr)
	}
}

func TestReadSnapshotSetCanceledReservationReleasesPartialReaders(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned physical reader fixture not configured")
	}
	access := testAccess(t, reader, 4)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := access.Reader().QueryContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()
	setBuilder := access.Reader().(db.ReadSnapshotSetBeginner)
	if _, err := setBuilder.BeginReadOnlySnapshotSet(cancelCtx, 4); err == nil {
		t.Fatal("reservation succeeded while one reader slot was held past its deadline")
	}
	if err := blocker.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for access.reader.Stats().InUse != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("canceled reservation leaked %d readers", got)
	}
	set, err := setBuilder.BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatalf("reservation after canceled partial attempt: %v", err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReadSnapshotSetConcurrentGroupsUseReaderPoolCap(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned physical reader fixture not configured")
	}
	access := testAccess(t, reader, 8)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	setBuilder := access.Reader().(db.ReadSnapshotSetBeginner)
	type result struct {
		set db.ReadSnapshotSet
		err error
	}
	results := make(chan result, 2)
	closed := make(chan error, 2)
	start := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			set, err := setBuilder.BeginReadOnlySnapshotSet(ctx, 4)
			results <- result{set: set, err: err}
			if err != nil {
				return
			}
			<-release
			closed <- set.Close()
		}()
	}
	close(start)
	sets := make([]db.ReadSnapshotSet, 0, 2)
	for range 2 {
		select {
		case got := <-results:
			if got.err != nil {
				t.Fatalf("begin concurrent set: %v", got.err)
			}
			sets = append(sets, got.set)
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent snapshot sets did not reserve the available reader pool")
		}
	}
	if got := access.reader.Stats().InUse; got != 8 {
		t.Fatalf("concurrent set readers=%d, want pool cap 8", got)
	}
	releaseOnce.Do(func() { close(release) })
	for range sets {
		if err := <-closed; err != nil {
			t.Fatalf("close concurrent set: %v", err)
		}
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("concurrent snapshot sets leaked %d readers", got)
	}
}

func TestReadSnapshotSetCanceledGateWaitDoesNotLeak(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned physical reader fixture not configured")
	}
	access := testAccess(t, reader, 4)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	setBuilder := access.Reader().(db.ReadSnapshotSetBeginner)
	blocker, err := access.Reader().QueryContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	firstCtx, cancelFirst := context.WithCancel(ctx)
	firstResult := make(chan error, 1)
	go func() {
		_, err := setBuilder.BeginReadOnlySnapshotSet(firstCtx, 4)
		firstResult <- err
	}()
	deadline := time.Now().Add(time.Second)
	for access.reader.Stats().InUse < 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := access.reader.Stats().InUse; got != 4 {
		cancelFirst()
		_ = blocker.Close()
		t.Fatalf("first set did not hold three partial readers beside blocker: in_use=%d", got)
	}
	waitCtx, cancelWait := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancelWait()
	if _, err := setBuilder.BeginReadOnlySnapshotSet(waitCtx, 1); err == nil {
		cancelFirst()
		t.Fatal("second snapshot set passed the held per-Access reservation gate")
	}
	cancelFirst()
	if err := <-firstResult; err == nil {
		t.Fatal("partially reserved first set succeeded after cancellation")
	}
	if err := blocker.Close(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for access.reader.Stats().InUse != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("gate cancellation leaked %d readers", got)
	}
	next, err := setBuilder.BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatalf("reservation after canceled gate wait: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReadSnapshotSetCloseAfterCancellationIsIdempotent(t *testing.T) {
	pool := sql.OpenDB(terminalErrorConnector{})
	defer pool.Close()
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader := newReadTransaction(ctx, tx, conn, &Access{})
	set := &readSnapshotSet{readers: []*readTransaction{reader}}
	cancel()
	if err := set.Close(); err != nil {
		t.Fatalf("first close after cancellation: %v", err)
	}
	if err := set.Close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
	if _, err := set.Reader(0); !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("reader after close: %v", err)
	}
	if got := pool.Stats().InUse; got != 0 {
		t.Fatalf("closed set leaked %d connections", got)
	}
}

func TestReadSnapshotSetCloseRetainsCanceledRollbackError(t *testing.T) {
	rollbackErr := errors.New("seeded rollback failure")
	pool := sql.OpenDB(terminalErrorConnector{rollbackErr: rollbackErr})
	defer pool.Close()
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader := newReadTransaction(ctx, tx, conn, &Access{})
	set := &readSnapshotSet{readers: []*readTransaction{reader}}
	cancel()
	deadline := time.Now().Add(time.Second)
	for pool.Stats().InUse != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := pool.Stats().InUse; got != 0 {
		t.Fatalf("canceled set leaked %d connections", got)
	}
	if err := set.Close(); !errors.Is(err, rollbackErr) {
		t.Fatalf("close after canceled rollback=%v, want seeded error", err)
	}
	if err := set.Close(); !errors.Is(err, rollbackErr) {
		t.Fatalf("repeated close after canceled rollback=%v, want seeded error", err)
	}
}

func TestSnapshotSQLLiteralQuotesIdentifier(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
		want string
		bad  bool
	}{
		{name: "postgres snapshot", id: "00000003-0000001B-1", want: "'00000003-0000001B-1'"},
		{name: "quote escaped", id: "x'; SELECT 1; --", want: "'x''; SELECT 1; --'"},
		{name: "empty rejected", bad: true},
		{name: "nul rejected", id: "snapshot\x00id", bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := snapshotSQLLiteral(tc.id)
			if tc.bad {
				if err == nil {
					t.Fatalf("snapshot id %q accepted", tc.id)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("snapshot literal=%q error=%v, want %q", got, err, tc.want)
			}
		})
	}
}
