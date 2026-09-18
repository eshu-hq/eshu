// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// poolExhaustionProbeDB is a Beginner that enforces a hard cap on
// simultaneous open transactions. Once capacity is reached, Begin blocks until
// a slot is released or the caller's context expires. It records the peak
// concurrent opens and whether any caller was blocked.
//
// This is a behavioral-spec fake: it exercises the pool-blocking contract in
// isolation (not against a real *sql.DB). Tests that need to guard the actual
// database/sql pool should constrain a real *sql.DB with SetMaxOpenConns(N)
// and a test Postgres instance.
type poolExhaustionProbeDB struct {
	mu         sync.Mutex
	capacity   int
	open       int
	peakOpen   int
	beginCount int
	blockedAny bool // true if any Begin call was forced to wait

	// cond signals waiters when open < capacity or context expires.
	cond *sync.Cond
}

func newPoolExhaustionProbeDB(capacity int) *poolExhaustionProbeDB {
	database := &poolExhaustionProbeDB{capacity: capacity}
	database.cond = sync.NewCond(&database.mu)
	return database
}

func (database *poolExhaustionProbeDB) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return &queueFakeRows{}, nil
}

func (database *poolExhaustionProbeDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (database *poolExhaustionProbeDB) Begin(ctx context.Context) (db.Transaction, error) {
	database.mu.Lock()
	database.beginCount++

	// Monitor context expiration in a goroutine. Must broadcast under mu
	// to avoid a lost wakeup: if ctx fires between the (open >= capacity)
	// check and Cond.Wait, a broadcast outside the lock may wake nobody.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			database.mu.Lock()
			database.cond.Broadcast()
			database.mu.Unlock()
		case <-done:
		}
	}()

	wasBlocked := false
	// Wait while pool is full and context is still alive.
	for database.open >= database.capacity {
		wasBlocked = true
		database.cond.Wait()
		if ctx.Err() != nil {
			database.mu.Unlock()
			return nil, ctx.Err()
		}
	}

	database.open++
	if database.open > database.peakOpen {
		database.peakOpen = database.open
	}
	if wasBlocked {
		database.blockedAny = true
	}
	database.mu.Unlock()

	return &poolExhaustionProbeTx{database: database}, nil
}

type poolExhaustionProbeTx struct {
	database *poolExhaustionProbeDB
	released bool
}

func (tx *poolExhaustionProbeTx) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return &queueFakeRows{}, nil
}

func (tx *poolExhaustionProbeTx) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (tx *poolExhaustionProbeTx) Commit() error   { return tx.release() }
func (tx *poolExhaustionProbeTx) Rollback() error { return tx.release() }

func (tx *poolExhaustionProbeTx) release() error {
	tx.database.mu.Lock()
	if !tx.released {
		tx.database.open--
		tx.released = true
		tx.database.cond.Signal() // wake one blocked waiter
	}
	tx.database.mu.Unlock()
	return nil
}

// TestPoolExhaustionNPlusOneBlocksThenProceeds is a behavioral-spec test:
// against a bounded-capacity fake pool, the N+1th goroutine blocks when the
// pool is full, then proceeds when a slot is released. This does not exercise
// a real *sql.DB; it validates the pool-blocking contract in isolation.
func TestPoolExhaustionNPlusOneBlocksThenProceeds(t *testing.T) {
	t.Parallel()

	const capacity = 5
	database := newPoolExhaustionProbeDB(capacity)

	var wg sync.WaitGroup
	errs := make([]error, capacity+1)
	startGate := make(chan struct{})

	// Launch capacity+1 goroutines, each acquiring a transaction.
	for i := 0; i < capacity+1; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-startGate // rendezvous

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			tx, err := database.Begin(ctx)
			if err != nil {
				errs[id] = fmt.Errorf("goroutine %d Begin: %w", id, err)
				return
			}
			// Hold the transaction briefly so the pool stays full.
			time.Sleep(20 * time.Millisecond)
			if rerr := tx.Rollback(); rerr != nil {
				errs[id] = fmt.Errorf("goroutine %d Rollback: %w", id, rerr)
				return
			}
		}(i)
	}

	close(startGate) // release all goroutines at once
	wg.Wait()

	// Every goroutine must have succeeded (no errors).
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d failed: %v", i, err)
		}
	}

	// Peak open must equal capacity (all capacity slots were filled).
	if database.peakOpen != capacity {
		t.Fatalf("peakOpen = %d, want capacity %d", database.peakOpen, capacity)
	}

	// At least one goroutine must have blocked (the N+1th).
	if !database.blockedAny {
		t.Fatal("expected at least one caller to block when pool was at capacity")
	}

	// All goroutines must have acquired a transaction.
	if database.beginCount != capacity+1 {
		t.Fatalf("beginCount = %d, want %d", database.beginCount, capacity+1)
	}
}

// TestPoolExhaustionContextTimeoutSurfacesWhenStuck proves that a goroutine
// that cannot acquire a slot before its context expires gets a
// deadline-exceeded error from the bounded pool.
func TestPoolExhaustionContextTimeoutSurfacesWhenStuck(t *testing.T) {
	t.Parallel()

	const capacity = 2
	database := newPoolExhaustionProbeDB(capacity)

	startGate := make(chan struct{})

	// Hold all capacity slots indefinitely.
	holdersAcquired := make(chan struct{}, capacity)
	holdersDone := make(chan struct{})
	var holdersWg sync.WaitGroup
	for i := 0; i < capacity; i++ {
		holdersWg.Add(1)
		go func() {
			defer holdersWg.Done()
			<-startGate
			tx, _ := database.Begin(context.Background())
			holdersAcquired <- struct{}{} // signal slot acquired
			<-holdersDone
			_ = tx.Rollback()
		}()
	}

	close(startGate)
	// Wait until all holders have acquired slots.
	for i := 0; i < capacity; i++ {
		<-holdersAcquired
	}

	// N+1th goroutine with a short timeout — must fail.
	nPlusOneDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := database.Begin(ctx)
		nPlusOneDone <- err
	}()

	// Wait for the N+1th goroutine to finish (should time out).
	var nPlusOneErr error
	select {
	case nPlusOneErr = <-nPlusOneDone:
	case <-time.After(2 * time.Second):
		t.Fatal("N+1th goroutine did not finish within 2s")
	}

	if nPlusOneErr == nil {
		t.Fatal("N+1th goroutine: expected error from blocked Begin with short context timeout, got nil")
	}
	if !errors.Is(nPlusOneErr, context.DeadlineExceeded) {
		t.Fatalf("N+1th goroutine error: %v; want context.DeadlineExceeded (or wrapped)", nPlusOneErr)
	}

	// Release holders so test can clean up.
	close(holdersDone)
	holdersWg.Wait()
}
