package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueHeartbeatRenewsClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 23, 17, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{execResults: []sql.Result{fakeResult{}}}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	err := queue.Heartbeat(context.Background(), reducer.Intent{IntentID: "intent-1"})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec calls = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"claim_until = $1",
		"updated_at = $2",
		"work_item_id = $3",
		"lease_owner = $4",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Heartbeat() query missing %q:\n%s", want, query)
		}
	}

	if got, want := db.execs[0].args[0], now.Add(time.Minute); got != want {
		t.Fatalf("claim_until arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[1], now; got != want {
		t.Fatalf("updated_at arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[2], "intent-1"; got != want {
		t.Fatalf("work_item_id arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], "reducer-1"; got != want {
		t.Fatalf("lease_owner arg = %v, want %v", got, want)
	}
}

func TestReducerQueueHeartbeatRejectsMissingClaim(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{zeroRowsResult{}}}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
	}

	err := queue.Heartbeat(context.Background(), reducer.Intent{IntentID: "intent-1"})
	if err == nil {
		t.Fatal("Heartbeat() error = nil, want non-nil")
	}
	if err != ErrReducerClaimRejected {
		t.Fatalf("Heartbeat() error = %v, want %v", err, ErrReducerClaimRejected)
	}
}

type zeroRowsResult struct{}

func (zeroRowsResult) LastInsertId() (int64, error) { return 0, nil }

func (zeroRowsResult) RowsAffected() (int64, error) { return 0, nil }
