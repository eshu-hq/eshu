// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7427 PR3 disposable PostgreSQL 18 proof that migration 191 beside a continuous terms writer and a back-to-back orphan census never fails either stream, each attempt drops the key or returns 55P03 with all 65 keys kept, the slowest insert stays under 1s with no other holder, and beside a 1.5s scope_generations holder the wait is bounded by that holder (well under the 5s lock_timeout); enrolled in the live-postgres-readiness runner; reads ESHU_GENERATION_RETENTION_PROOF_DSN (with its DISPOSABLE flag). local run passed in 4.1s of test time on PostgreSQL 18.6 on 2026-10-16; wall time is logged, not gated
//eshu:readiness:env ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
)

// contentionLoad runs a terms writer (one autocommit insert every 5 ms, the
// search document writer's statement shape) and a retention-style reader (the
// orphan census back to back: it reads every terms leaf and
// scope_generations) until stop, and records the slowest statement of each.
type contentionLoad struct {
	mu         sync.Mutex
	slowWrite  time.Duration
	slowRead   time.Duration
	writes     int
	reads      int
	loadErrors []error
	stop       chan struct{}
	done       sync.WaitGroup
}

func startContentionLoad(t *testing.T, f *liveFixture) *contentionLoad {
	t.Helper()
	load := &contentionLoad{stop: make(chan struct{})}
	nonce := time.Now().UnixNano()
	load.done.Add(2)
	go func() {
		defer load.done.Done()
		for i := 0; ; i++ {
			select {
			case <-load.stop:
				return
			default:
			}
			started := time.Now()
			_, err := f.db.ExecContext(f.ctx, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, 'doc-load', $3, 'term-load', 1)`, f.scope, f.active, fmt.Sprintf("tk-load-%d-%d", nonce, i))
			load.record(&load.slowWrite, &load.writes, time.Since(started), err)
			time.Sleep(5 * time.Millisecond)
		}
	}()
	go func() {
		defer load.done.Done()
		for {
			select {
			case <-load.stop:
				return
			default:
			}
			started := time.Now()
			_, err := indexretention.TakeCensus(f.ctx, f.database)
			load.record(&load.slowRead, &load.reads, time.Since(started), err)
			time.Sleep(5 * time.Millisecond)
		}
	}()
	time.Sleep(300 * time.Millisecond)
	return load
}

func (l *contentionLoad) record(slowest *time.Duration, count *int, took time.Duration, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	*count++
	if took > *slowest {
		*slowest = took
	}
	if err != nil {
		l.loadErrors = append(l.loadErrors, err)
	}
}

func (l *contentionLoad) finish(t *testing.T) (slowWrite, slowRead time.Duration) {
	t.Helper()
	time.Sleep(200 * time.Millisecond)
	close(l.stop)
	l.done.Wait()
	if len(l.loadErrors) != 0 {
		t.Fatalf("the writer and reader failed beside the drop migration: %v", l.loadErrors)
	}
	if l.writes == 0 || l.reads == 0 {
		t.Fatalf("load ran %d writes and %d reads, want both: the overlap did not happen", l.writes, l.reads)
	}
	return l.slowWrite, l.slowRead
}

// runDropAttempt runs the migration once and fails unless it succeeded or
// returned the 55P03 the runner retries. It returns whether it dropped the
// key and how long the attempt took.
func runDropAttempt(t *testing.T, f *liveFixture, conn *sql.Conn, migration string) (bool, time.Duration) {
	t.Helper()
	started := time.Now()
	_, err := conn.ExecContext(f.ctx, migration)
	took := time.Since(started)
	switch {
	case err == nil:
		return true, took
	case coordination.IsLockNotAvailable(err):
		if got := len(f.termsGenerationForeignKeys(t)); got != 65 {
			t.Fatalf("terms foreign keys after a refused attempt = %d, want all 65: the drop must be all or nothing", got)
		}
		return false, took
	default:
		t.Fatalf("drop attempt beside the writer and reader: %v, want success or SQLSTATE 55P03", err)
		return false, took
	}
}

// TestTermsForeignKeyDropLiveBoundsConcurrentWriters measures what the drop
// migration costs a concurrent terms writer and a retention-style reader.
//
// Without a long holder, each attempt either drops the key, holding ACCESS
// EXCLUSIVE on scope_generations, the terms parent and its leaves for the
// catalog change only (8-16 ms measured), or fails at once with 55P03 because
// a terms statement is in flight; the runner retries 55P03. Neither statement
// stream fails, and neither waits more than one census read plus that hold.
//
// With a transaction holding scope_generations, the attempt waits for it
// while holding nothing, and every new scope_generations user (a terms
// insert's key check, the census anti-join) queues behind that wait. The wait
// is bounded by the shorter of the holder and lock_timeout (5 s, the
// runner's value).
func TestTermsForeignKeyDropLiveBoundsConcurrentWriters(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.active, 50, 4)
	f.seedIndex(t, f.old, 50, 4)
	migration := postgres.MigrationSQL(termsForeignKeyDrop)
	conn, _ := f.migrationConn(t)

	f.restoreTermsForeignKey(t, termsForeignKeyNames[0])
	load := startContentionLoad(t, f)
	attempts, dropped := 0, false
	var dropTook time.Duration
	for attempts < 40 && !dropped {
		attempts++
		dropped, dropTook = runDropAttempt(t, f, conn, migration)
		time.Sleep(100 * time.Millisecond)
	}
	slowWrite, slowRead := load.finish(t)
	t.Logf("no holder: dropped=%v after %d attempts (successful attempt %s); slowest write %s, slowest census %s",
		dropped, attempts, dropTook, slowWrite, slowRead)
	if !dropped {
		t.Fatalf("the drop never applied in %d attempts beside the writer and reader", attempts)
	}
	if slowWrite > time.Second {
		t.Fatalf("slowest terms insert beside the drop = %s, want under 1s (measured hold 8-16 ms)", slowWrite)
	}

	f.restoreTermsForeignKey(t, termsForeignKeyNames[0])
	load = startContentionLoad(t, f)
	holder, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin scope_generations holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(f.ctx, `SELECT generation_id FROM scope_generations WHERE generation_id = $1 FOR UPDATE`, f.old); err != nil {
		t.Fatalf("holder locks the generation row: %v", err)
	}
	const holdFor = 1500 * time.Millisecond
	released := make(chan struct{})
	go func() {
		time.Sleep(holdFor)
		_ = holder.Commit()
		close(released)
	}()
	dropped, dropTook = runDropAttempt(t, f, conn, migration)
	<-released
	slowWrite, slowRead = load.finish(t)
	t.Logf("held %s: dropped=%v in %s; slowest write %s, slowest census %s", holdFor, dropped, dropTook, slowWrite, slowRead)
	if bound := holdFor + time.Second; slowWrite > bound || slowRead > bound+time.Second {
		t.Fatalf("slowest write %s, census %s beside a waiting drop, want each within the %s hold plus 1s", slowWrite, slowRead, holdFor)
	}
	if dropTook > 5*time.Second+time.Second {
		t.Fatalf("the drop attempt took %s, want it bounded by the 5s lock_timeout", dropTook)
	}
	if !dropped {
		if ok, _ := runDropAttempt(t, f, conn, migration); !ok {
			t.Fatal("the drop did not apply on a quiet retry")
		}
	}
	if got := f.termsGenerationForeignKeys(t); len(got) != 0 {
		t.Fatalf("terms foreign keys after the drop = %v, want none", got)
	}
}
