// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// ackTriggerRow is the queue state the migration 093 replay trigger
// (enforce_cross_scope_required_replay) decides.
type ackTriggerRow struct {
	status, leaseOwner, failureClass string
	flag, reopened                   bool
	attempts                         int
}

func readAckTriggerRow(t *testing.T, ctx context.Context, db *sql.DB, id string) ackTriggerRow {
	t.Helper()
	var (
		row          ackTriggerRow
		lease, class sql.NullString
		reopened     sql.NullTime
	)
	if err := db.QueryRowContext(ctx, `
SELECT status, lease_owner, failure_class, cross_scope_replay_required, reopened_at, attempt_count
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&row.status, &lease, &class, &row.flag, &reopened, &row.attempts); err != nil {
		t.Fatalf("read item %s: %v", id, err)
	}
	row.leaseOwner, row.failureClass, row.reopened = lease.String, class.String, reopened.Valid
	return row
}

// ackTriggerSucceeded is the result a pass hands the queue on success.
func ackTriggerSucceeded(intent reducercontract.Intent) reducercontract.Result {
	return reducercontract.Result{IntentID: intent.IntentID, Domain: intent.Domain, Status: reducercontract.ResultStatusSucceeded}
}

// TestAckReplayTriggerMatrixLive is the SQL-level branch matrix of the
// migration 093 trigger that turns a flagged acknowledgement into a re-pend
// (#10972 reality audit, risk 6). Every row is claimed through the real
// ReducerQueue.Claim and flagged through the real holder reopen statement, then
// ended through the real Ack or Fail. The trigger fires only when the OLD row
// is a flagged claimed or running reducer item:
//   - flagged + succeeded: back to pending, attempt_count 0, lease and failure
//     cleared, reopened_at stamped, flag cleared; the next claim is attempt 1
//     and an unflagged ack then settles the item (the fixpoint);
//   - flagged + retrying or dead_letter: the new status stands, the flag clears;
//   - unflagged + succeeded: the trigger does not fire.
//
// Expected on main: GREEN. It pins the shipped trigger; no code changes.
func TestAckReplayTriggerMatrixLive(t *testing.T) {
	for _, tc := range []struct {
		name         string
		running      bool
		flagged      bool
		end          string // ack, fail-retry, fail-dead
		wantStatus   string
		wantReopened bool
	}{
		{name: "claimed flagged ack re-pends", flagged: true, end: "ack", wantStatus: "pending", wantReopened: true},
		{name: "running flagged ack re-pends", running: true, flagged: true, end: "ack", wantStatus: "pending", wantReopened: true},
		{name: "claimed unflagged ack settles", end: "ack", wantStatus: "succeeded"},
		{name: "claimed flagged retryable failure retries", flagged: true, end: "fail-retry", wantStatus: "retrying"},
		{name: "claimed flagged terminal failure dead-letters", flagged: true, end: "fail-dead", wantStatus: "dead_letter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db := fanOutLiveDB(t)
			queue := fanOutLiveQueue(db)
			const scope = "scope:10972:ack-trigger"
			fanOutSeedScope(t, ctx, db, scope, "g1")
			id := fanOutSeedItem(t, ctx, db, scope, "g1")
			intent, ok, err := queue.Claim(ctx)
			if err != nil || !ok || intent.IntentID != id {
				t.Fatalf("claim = %q %v %v, want %q", intent.IntentID, ok, err, id)
			}
			if tc.running {
				if err := queue.Heartbeat(ctx, intent); err != nil {
					t.Fatalf("heartbeat: %v", err)
				}
				if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'running' WHERE work_item_id = $1`, id); err != nil {
					t.Fatal(err)
				}
			}
			if tc.flagged {
				reopen, err := fanOutLiveStore(db).ReopenHolderDetailed(ctx, scope, fanOutLiveOrigin)
				if err != nil {
					t.Fatalf("reopen: %v", err)
				}
				if got := readAckTriggerRow(t, ctx, db, id); !got.flag {
					t.Fatalf("after reopen (%s) row = %+v, want the replay bit set on the claimed row", reopen.Outcome, got)
				}
			}
			switch tc.end {
			case "ack":
				err = queue.Ack(ctx, intent, ackTriggerSucceeded(intent))
			case "fail-retry":
				err = queue.Fail(ctx, intent, raceRetryableErr{})
			case "fail-dead":
				err = queue.Fail(ctx, intent, errors.New("terminal: not retryable"))
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.end, err)
			}
			got := readAckTriggerRow(t, ctx, db, id)
			if got.status != tc.wantStatus || got.flag {
				t.Fatalf("row = %+v, want status %s with the flag cleared", got, tc.wantStatus)
			}
			if got.reopened != tc.wantReopened {
				t.Fatalf("row = %+v, want reopened_at set = %v", got, tc.wantReopened)
			}
			if tc.wantStatus != "pending" {
				return
			}
			if got.attempts != 0 || got.leaseOwner != "" || got.failureClass != "" {
				t.Fatalf("re-pended row = %+v, want attempts 0, no lease, no failure class", got)
			}
			// Fixpoint: the re-pend runs exactly once more, then settles.
			again, ok, err := queue.Claim(ctx)
			if err != nil || !ok || again.IntentID != id || again.AttemptCount != 1 {
				t.Fatalf("re-claim = %q attempt %d %v %v, want %q at attempt 1", again.IntentID, again.AttemptCount, ok, err, id)
			}
			if err := queue.Ack(ctx, again, ackTriggerSucceeded(again)); err != nil {
				t.Fatal(err)
			}
			if got := readAckTriggerRow(t, ctx, db, id); got.status != "succeeded" || got.flag {
				t.Fatalf("after the replayed pass row = %+v, want succeeded", got)
			}
			if _, ok, err := queue.Claim(ctx); err != nil || ok {
				t.Fatalf("claim after settling = %v %v, want nothing claimable", ok, err)
			}
		})
	}
}
