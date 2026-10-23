// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8750 disposable PostgreSQL 18 proof that the destructive-successor guard re-reads its prior on the commit transaction after the scope row lock: a same-scope commit or Ack that lands between the pre-tx sizing and the lock is re-sized (W1, W2), an unchanged scope costs one read (W3), and the held scope row blocks a concurrent commit and Ack until the guarded commit ends (W4); reuses the #7978 bootstrapped proof schema; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// windowBeginner runs hook once, then opens the guarded commit's
// transaction and reports its backend pid. The hook lands exactly in the
// #8750 window: the pre-tx sizing has returned and the commit holds no lock
// yet. Later Begins (the post-commit backfill) delegate plainly.
type windowBeginner struct {
	t     *testing.T
	base  db.Beginner
	hook  func()
	fired bool
	pids  chan int
}

func (b *windowBeginner) Begin(ctx context.Context) (db.Transaction, error) {
	if b.fired {
		return b.base.Begin(ctx)
	}
	b.fired = true
	if b.hook != nil {
		b.hook()
	}
	tx, err := b.base.Begin(ctx)
	if err != nil || b.pids == nil {
		return tx, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT pg_backend_pid()")
	if err != nil {
		return nil, errors.Join(err, tx.Rollback())
	}
	defer func() { _ = rows.Close() }()
	var pid int
	if rows.Next() {
		if err := rows.Scan(&pid); err != nil {
			return nil, errors.Join(err, tx.Rollback())
		}
	}
	b.pids <- pid
	return tx, rows.Err()
}

// hookWindow installs hook in the proof store's Begin and captures its
// commit-stage logs.
func (p *reprojectProof) hookWindow(hook func()) (*windowBeginner, *bytes.Buffer) {
	beginner := &windowBeginner{t: p.t, base: SQLDB{DB: p.db}, hook: hook}
	logs := &bytes.Buffer{}
	p.store.beginner = beginner
	p.store.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	return beginner, logs
}

// commitWith commits generation through store, a second committer on the
// proof scope.
func (p *reprojectProof) commitWith(store IngestionStore, generation scope.ScopeGeneration, files, entities int) error {
	envelopes := guardSizingEnvelopes(generation, guardSizingFiles("f", files), entities, nil, nil, false)
	stream := make(chan facts.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		stream <- envelope
	}
	close(stream)
	return store.CommitScopeGeneration(context.Background(), p.scope, generation, stream)
}

// generationStatus reads one generation's status.
func (p *reprojectProof) generationStatus(generationID string) string {
	p.t.Helper()
	var status string
	if err := p.db.QueryRowContext(context.Background(),
		`SELECT status FROM scope_generations WHERE generation_id = $1`, generationID).Scan(&status); err != nil {
		p.t.Fatalf("read generation %s status: %v", generationID, err)
	}
	return status
}

// windowStageLog returns the first JSON log record for stage.
func windowStageLog(t *testing.T, logs *bytes.Buffer, stage string) map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(logs.Bytes()))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode log line %q: %v", scanner.Text(), err)
		}
		if record["stage"] == stage {
			return record
		}
	}
	t.Fatalf("no %q stage log in:\n%s", stage, logs.String())
	return nil
}

func requireLogFields(t *testing.T, record map[string]any, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if record[key] != value {
			t.Fatalf("%s %s = %v, want %v: %v", record["stage"], key, record[key], value, record)
		}
	}
}

// openWindowCommitInterleave seeds A (100 collector rows, active) and
// builds G2, a 1000-row full the hook commits through a second store after
// S's pre-tx sizing read A alone.
func openWindowCommitInterleave(t *testing.T) (*reprojectProof, *bytes.Buffer) {
	t.Helper()
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(50, 0)
	g2 := p.generation("gen-g2", "hint-g2", "sha-g2", "interval_elapsed")
	second := NewIngestionStore(SQLDB{DB: p.db})
	_, logs := p.hookWindow(func() {
		if err := p.commitWith(second, g2, 500, 500); err != nil {
			t.Fatalf("commit gen-g2 in the window: %v", err)
		}
	})
	return p, logs
}

// TestGuardRecheckRefusesAfterACommitInTheWindowLive (W1): S's pre-tx
// sizing reads only A (100 rows). G2 (1000 rows) commits before S locks the
// scope. S's 60 rows pass against A but not against G2, which the claim
// sweep would let S supersede unprojected: refused.
func TestGuardRecheckRefusesAfterACommitInTheWindowLive(t *testing.T) {
	p, logs := openWindowCommitInterleave(t)
	p.commitGuardSizing(p.generation("gen-s", "hint-s", "sha-s", "interval_elapsed"), guardSizingFiles("f", 30), 30, nil, nil)

	p.requireGuardOutcome("gen-s", false, 1)
	if got := p.generationStatus("gen-g2"); got != "pending" {
		t.Fatalf("gen-g2 status = %s, want pending", got)
	}
	if got := p.generationStatus("gen-a"); got != "active" {
		t.Fatalf("gen-a status = %s, want active", got)
	}
	requireLogFields(t, windowStageLog(t, logs, "successor_guard"), map[string]any{
		"prior_sizing_generation_id": "gen-g2", "refused": true,
	})
	record := windowStageLog(t, logs, "successor_prior_rechecked")
	requireLogFields(t, record, map[string]any{
		"changed": true, "previous_candidate_generation_id": "gen-a", "candidate_generation_id": "gen-g2",
	})
	t.Logf("in-tx recheck with re-size: %v s, of which re-size %v s", record["duration_seconds"], record["resize_duration_seconds"])
}

// TestGuardRecheckStoresAHalfSizeFullAfterTheResizeLive (W1b): the re-size
// does not over-refuse: 600 rows hold half of G2 (1200 >= 1000).
func TestGuardRecheckStoresAHalfSizeFullAfterTheResizeLive(t *testing.T) {
	p, logs := openWindowCommitInterleave(t)
	p.commitGuardSizing(p.generation("gen-s", "hint-s", "sha-s", "interval_elapsed"), guardSizingFiles("f", 300), 300, nil, nil)

	p.requireGuardOutcome("gen-s", true, 0)
	requireLogFields(t, windowStageLog(t, logs, "successor_prior_rechecked"), map[string]any{"changed": true})
}

// TestGuardRecheckResizesAfterAnAckInTheWindowLive (W2): A active (100),
// G2 a pending 1000-row full, P a newer pending delta over A that spares G2
// (#7473). S's pre-tx prior is max(P's union, A), about 106. The hook claims
// and Acks G2, so G2 is active when S locks the scope: refused against G2.
func TestGuardRecheckResizesAfterAnAckInTheWindowLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(50, 0)
	p.commitGuardSizing(p.generation("gen-g2", "hint-g2", "sha-g2", "interval_elapsed"), guardSizingFiles("f", 500), 500, nil, nil)
	changed := guardSizingPaths("f", 0, 5)
	p.commitGuardSizing(p.deltaGeneration("gen-p", "hint-p", "sha-p", "sha-a"), guardSizingLive(changed), 0, changed, nil)
	p.requireGuardOutcome("gen-p", true, 0)

	_, logs := p.hookWindow(func() {
		ctx := context.Background()
		queue := NewProjectorQueue(SQLDB{DB: p.db}, "proof-worker", time.Minute)
		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("claim in the window: ok=%v err=%v", ok, err)
		}
		if work.Generation.GenerationID != "gen-g2" {
			t.Fatalf("claimed %s, want gen-g2 (the older full held ahead of the delta)", work.Generation.GenerationID)
		}
		if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
			t.Fatalf("ack gen-g2 in the window: %v", err)
		}
	})
	p.commitGuardSizing(p.generation("gen-s", "hint-s", "sha-s", "interval_elapsed"), guardSizingFiles("f", 30), 30, nil, nil)

	p.requireGuardOutcome("gen-s", false, 1)
	if got := p.generationStatus("gen-g2"); got != "active" {
		t.Fatalf("gen-g2 status = %s, want active", got)
	}
	requireLogFields(t, windowStageLog(t, logs, "successor_guard"), map[string]any{
		"prior_sizing": "active", "prior_sizing_generation_id": "gen-g2", "refused": true,
	})
	requireLogFields(t, windowStageLog(t, logs, "successor_prior_rechecked"), map[string]any{
		"changed": true, "recheck_reads": float64(2), "candidate_generation_id": "gen-p",
		"previous_active_generation_id": "gen-a", "active_generation_id": "gen-g2",
	})
}

// TestGuardRecheckUnchangedScopeReadsOnceLive (W3): no interleave. S's 60
// rows hold half of A's 100 and store; the recheck saw no change in one
// read.
func TestGuardRecheckUnchangedScopeReadsOnceLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(50, 0)
	_, logs := p.hookWindow(nil)
	p.commitGuardSizing(p.generation("gen-s", "hint-s", "sha-s", "interval_elapsed"), guardSizingFiles("f", 30), 30, nil, nil)

	p.requireGuardOutcome("gen-s", true, 0)
	record := windowStageLog(t, logs, "successor_prior_rechecked")
	requireLogFields(t, record, map[string]any{
		"changed": false, "recheck_reads": float64(1), "candidate_generation_id": "gen-a",
	})
	t.Logf("in-tx recheck, unchanged active candidate: %v s", record["duration_seconds"])
}

// waitForScopeWaiter polls until a backend running a statement that contains
// fragment waits on a heavyweight lock held by holderPID: the commit's scope
// row. done reports the waiter finishing early, which fails the proof.
func waitForScopeWaiter(t *testing.T, ctx context.Context, database *sql.DB, holderPID int, fragment string, done <-chan error) {
	t.Helper()
	for {
		select {
		case err := <-done:
			t.Fatalf("%q finished before blocking on the scope row (err=%v)", fragment, err)
		default:
		}
		var waiterPID int
		var waitEvent string
		err := database.QueryRowContext(ctx, `
SELECT pid, COALESCE(wait_event, '')
FROM pg_stat_activity
WHERE $1::int = ANY(pg_blocking_pids(pid))
  AND wait_event_type = 'Lock'
  AND strpos(query, $2) > 0
LIMIT 1`, holderPID, fragment).Scan(&waiterPID, &waitEvent)
		if err == nil {
			t.Logf("%q: backend %d waits on backend %d (wait_event_type=Lock, wait_event=%s)", fragment, waiterPID, holderPID, waitEvent)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("poll scope waiters: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%q never blocked on backend %d: %v", fragment, holderPID, ctx.Err())
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// receive waits for one result or fails on the test deadline.
func receive(t *testing.T, ctx context.Context, what string, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		t.Fatalf("%s did not finish: %v", what, ctx.Err())
		return nil
	}
}

// TestGuardRecheckScopeRowBlocksCommitAndAckLive (W4) is the live proof of
// the window ruling's freeze claim. S commits through the production path
// with an unbuffered stream the test holds after the first envelope, so S is
// parked in its fact loop holding the scope row it locked in the scope
// upsert. A second commit (G2) and an Ack of the pending P, each on its own
// connection, block on S's backend with wait_event_type = 'Lock'. The Ack
// sets a transaction-local lock_timeout, so it gives up with
// ErrWorkAckDeferred and P stays pending; G2 has no timeout and finishes
// only after S commits; the re-Ack then publishes P. S's recheck saw P as
// its candidate and no G2.
func TestGuardRecheckScopeRowBlocksCommitAndAckLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(50, 0)
	p.commitGuardSizing(p.generation("gen-p", "hint-p", "sha-p", "interval_elapsed"), guardSizingFiles("f", 50), 50, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	queue := NewProjectorQueue(SQLDB{DB: p.db}, "proof-worker", time.Minute)
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != "gen-p" {
		t.Fatalf("claim gen-p: ok=%v err=%v work=%s", ok, err, work.Generation.GenerationID)
	}

	beginner, logs := p.hookWindow(nil)
	beginner.pids = make(chan int, 1)
	sGeneration := p.generation("gen-s", "hint-s", "sha-s", "interval_elapsed")
	envelopes := guardSizingEnvelopes(sGeneration, guardSizingFiles("f", 50), 50, nil, nil, false)
	stream := make(chan facts.Envelope)
	sDone := make(chan error, 1)
	go func() { sDone <- p.store.CommitScopeGeneration(ctx, p.scope, sGeneration, stream) }()
	var sPID int
	select {
	case sPID = <-beginner.pids:
	case err := <-sDone:
		t.Fatalf("gen-s finished before opening its transaction: %v", err)
	case <-ctx.Done():
		t.Fatal("gen-s never opened its transaction")
	}
	// The unbuffered send returns once S's fact loop took the envelope: S is
	// past the scope upsert, the recheck, and the generation upsert.
	select {
	case stream <- envelopes[0]:
	case err := <-sDone:
		t.Fatalf("gen-s finished before reading its stream: %v", err)
	case <-ctx.Done():
		t.Fatal("gen-s never read its stream")
	}

	g2Done := make(chan error, 1)
	second := NewIngestionStore(SQLDB{DB: p.db})
	g2 := p.generation("gen-g2", "hint-g2", "sha-g2", "interval_elapsed")
	go func() { g2Done <- p.commitWith(second, g2, 50, 50) }()
	waitForScopeWaiter(t, ctx, p.db, sPID, "INSERT INTO ingestion_scopes", g2Done)
	ackDone := make(chan error, 1)
	go func() { ackDone <- queue.Ack(ctx, work, projectorruntime.Result{}) }()
	waitForScopeWaiter(t, ctx, p.db, sPID, "UPDATE ingestion_scopes", ackDone)

	ackWaitStart := time.Now()
	if err := receive(t, ctx, "ack behind gen-s", ackDone); !errors.Is(err, failure.ErrWorkAckDeferred) {
		t.Fatalf("Ack behind gen-s = %v, want ErrWorkAckDeferred", err)
	}
	t.Logf("Ack deferred by its lock_timeout %s after it was seen blocked", time.Since(ackWaitStart))
	if got := p.generationStatus("gen-p"); got != "pending" {
		t.Fatalf("gen-p status after the deferred Ack = %s, want pending", got)
	}
	select {
	case err := <-g2Done:
		t.Fatalf("gen-g2 finished while gen-s held the scope row: %v", err)
	default:
	}

	for _, envelope := range envelopes[1:] {
		select {
		case stream <- envelope:
		case err := <-sDone:
			t.Fatalf("gen-s finished before its stream drained: %v", err)
		}
	}
	close(stream)
	if err := receive(t, ctx, "gen-s commit", sDone); err != nil {
		t.Fatalf("commit gen-s: %v", err)
	}
	if err := receive(t, ctx, "gen-g2 commit", g2Done); err != nil {
		t.Fatalf("commit gen-g2 after gen-s: %v", err)
	}
	if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
		t.Fatalf("re-Ack gen-p after gen-s committed: %v", err)
	}
	p.requireGuardOutcome("gen-s", true, 0)
	if !p.generationStored("gen-g2") {
		t.Fatal("gen-g2 was not stored")
	}
	if got := p.generationStatus("gen-p"); got != "active" {
		t.Fatalf("gen-p status after the re-Ack = %s, want active", got)
	}
	requireLogFields(t, windowStageLog(t, logs, "successor_prior_rechecked"), map[string]any{
		"changed": false, "candidate_generation_id": "gen-p",
	})
}
