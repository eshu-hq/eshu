// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ackCommitMarker is the pseudo-statement commitMarkingDB records when the
// Ack's transaction commits, so a test can split in-tx from post-commit
// statements.
const ackCommitMarker = "-- COMMIT --"

// commitMarkingDB is ackStatementDB with a transaction whose Commit is
// recorded in the statement log.
type commitMarkingDB struct{ *ackStatementDB }

func (d commitMarkingDB) Begin(context.Context) (db.Transaction, error) {
	return commitMarkingTx(d), nil
}

type commitMarkingTx struct{ *ackStatementDB }

func (t commitMarkingTx) Commit() error {
	t.calls = append(t.calls, recordedExecCall{query: ackCommitMarker})
	return nil
}
func (commitMarkingTx) Rollback() error { return nil }

// ackWakeCall is one waiting-wake statement: its kind, key, and timestamp.
type ackWakeCall struct {
	kind string
	key  string
	at   time.Time
}

// splitAckWakes returns the waiting-wake statements sent before and after
// the commit marker.
func splitAckWakes(t *testing.T, calls []recordedExecCall) (inTx, postCommit []ackWakeCall) {
	t.Helper()
	committed := false
	for _, call := range calls {
		if call.query == ackCommitMarker {
			committed = true
			continue
		}
		if call.query != reopenWaitingRowsQuery {
			continue
		}
		at, ok := call.args[0].(time.Time)
		if !ok || len(call.args) != 3 {
			t.Fatalf("wake args = %v, want (time, kind, key)", call.args)
		}
		wake := ackWakeCall{kind: fmt.Sprint(call.args[1]), key: fmt.Sprint(call.args[2]), at: at}
		if committed {
			postCommit = append(postCommit, wake)
		} else {
			inTx = append(inTx, wake)
		}
	}
	if !committed {
		t.Fatal("the Ack never committed")
	}
	return inTx, postCommit
}

// TestAckPostCommitRewakeRepeatsTheInTxKeysAtTheWakeTime pins two #11228
// contracts on a scope whose collector kind publishes a producer domain, so
// the Ack wakes both generation_active and producer_domain_terminal keys:
// the post-commit re-wake sends exactly the in-tx wake's (kind, key) list,
// so the two cannot drift, and it stamps the reopened rows with the time of
// the re-wake, not the Ack's pre-transaction clock (ruling decision 2:
// visible_at and reopened_at move to the wake time).
func TestAckPostCommitRewakeRepeatsTheInTxKeysAtTheWakeTime(t *testing.T) {
	t.Parallel()
	database := commitMarkingDB{&ackStatementDB{}}
	queue := NewProjectorQueue(database, "projector-1", time.Minute)
	base := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	ticks := 0
	queue.Now = func() time.Time {
		ticks++
		return base.Add(time.Duration(ticks) * time.Second)
	}
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-1",
			CollectorKind: scope.CollectorKind(ackParkRaceCollectorKind),
		},
		Generation:   scope.ScopeGeneration{GenerationID: "generation-1"},
		AttemptCount: 1,
	}
	if err := queue.Ack(context.Background(), work, runtime.Result{}); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	inTx, postCommit := splitAckWakes(t, database.calls)
	if len(inTx) != 2 {
		t.Fatalf("in-tx wakes = %+v, want generation_active then producer_domain_terminal", inTx)
	}
	if inTx[0].kind != string(WaitingKindGenerationActive) || inTx[1].kind != string(WaitingKindProducerTerminal) {
		t.Fatalf("in-tx wake kinds = %q, %q", inTx[0].kind, inTx[1].kind)
	}
	if len(postCommit) != len(inTx) {
		t.Fatalf("post-commit wakes = %+v, want the in-tx list %+v", postCommit, inTx)
	}
	for i := range inTx {
		if postCommit[i].kind != inTx[i].kind || postCommit[i].key != inTx[i].key {
			t.Fatalf("post-commit wake %d = (%s, %s), want the in-tx (%s, %s)",
				i, postCommit[i].kind, postCommit[i].key, inTx[i].kind, inTx[i].key)
		}
		if !postCommit[i].at.After(inTx[i].at) {
			t.Fatalf("post-commit wake %d stamped %v, want the re-wake time, after the in-tx %v",
				i, postCommit[i].at, inTx[i].at)
		}
	}
}
