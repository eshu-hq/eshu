// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerQueueExpiryInterleaveLive controls the handler-to-ACK boundary
// while exercising production claim, heartbeat, ACK, and fault-expiry SQL.
// It is a queue mechanism shim, not a substitute for the real fault cell.
func TestReducerQueueExpiryInterleaveLive(t *testing.T) {
	for _, held := range []bool{false, true} {
		name := "ACK wins after claim poll"
		if held {
			name = "handler held through replacement claim"
		}
		t.Run(name, func(t *testing.T) {
			db := openReducerAckReclaimProofDB(t)
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			queue := ReducerQueue{database: SQLDB{DB: db}, LeaseOwner: "expiry-proof", LeaseDuration: time.Minute}
			seedReducerAckReclaimScope(t, ctx, db, "repository:7939")
			seedReducerAckReclaimGeneration(t, ctx, db, "repository:7939", "generation:7939")
			seedReducerAckReclaimWorkItem(t, ctx, db, "expiry-target", "repository:7939", "generation:7939")
			stale := claimOneReducerAckReclaimIntent(t, ctx, queue, "expiry-target")
			if err := queue.Heartbeat(ctx, stale); err != nil {
				t.Fatalf("startup heartbeat: %v", err)
			}
			entered, release, acked := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			go func() {
				close(entered)
				select {
				case <-release:
					acked <- queue.AckBatch(ctx, []reducer.Intent{stale}, nil)
				case <-ctx.Done():
					acked <- ctx.Err()
				}
			}()
			<-entered
			var observed int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM fact_work_items WHERE stage='reducer' AND status='claimed'").Scan(&observed); err != nil || observed != 1 {
				t.Fatalf("claim poll: count=%d err=%v", observed, err)
			}
			if !held {
				close(release)
				if err := <-acked; err != nil {
					t.Fatalf("ACK before expiry: %v", err)
				}
			}
			var schema string
			if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
				t.Fatal(err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			helper := filepath.Join(cwd, "../../../../scripts/lib/ifa_fault_reclaim_assert.sh")
			command := exec.CommandContext(ctx, "bash", "-c", `source "$1"; ifa_det_pg() { psql "$3" -v ON_ERROR_STOP=1 -Atc "$4"; }; ifa_fault_expire_reducer_claims proof 0 "$2" unused`, "proof", helper, os.Getenv("ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN"))
			command.Env = append(os.Environ(), "PGOPTIONS=-c search_path="+schema)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("production expiry helper: %v: %s", err, output)
			}
			want := "UPDATE 0"
			if held {
				want = "UPDATE 1"
			}
			if strings.TrimSpace(string(output)) != want {
				t.Fatalf("expiry result=%q want %q", output, want)
			}
			t.Logf("claimed poll=%d; production expiry=%s", observed, strings.TrimSpace(string(output)))
			if !held {
				return
			}
			// An independent item completes while the first handler is held.
			seedReducerAckReclaimWorkItem(t, ctx, db, "control", "repository:7939", "generation:7939")
			intents, err := queue.ClaimBatch(ctx, 8)
			if err != nil || len(intents) != 2 {
				t.Fatalf("competing claim: intents=%d err=%v", len(intents), err)
			}
			var fresh reducer.Intent
			for _, intent := range intents {
				if intent.IntentID == "expiry-target" {
					fresh = intent
				} else if err := queue.AckBatch(ctx, []reducer.Intent{intent}, nil); err != nil {
					t.Fatalf("control ACK: %v", err)
				}
			}
			if fresh.AttemptCount != 2 || fresh.ClaimedAt == nil || stale.ClaimedAt == nil || fresh.ClaimedAt.Equal(*stale.ClaimedAt) {
				t.Fatalf("replacement claim missing new epoch/attempt: %+v", fresh)
			}
			if err := queue.Heartbeat(ctx, stale); !errors.Is(err, reducer.ErrExecutionClaimRejected) {
				t.Fatalf("stale heartbeat=%v", err)
			}
			close(release)
			if err := <-acked; !errors.Is(err, reducer.ErrExecutionClaimRejected) {
				t.Fatalf("stale ACK=%v", err)
			}
			if err := queue.AckBatch(ctx, []reducer.Intent{fresh}, nil); err != nil {
				t.Fatalf("fresh ACK: %v", err)
			}
			status, owner := readReducerAckReclaimWorkItem(t, ctx, db, "expiry-target")
			if status != "succeeded" || owner != "" {
				t.Fatalf("terminal row=%s/%s", status, owner)
			}
			t.Log("replacement attempt=2; control completed while held; stale heartbeat/ACK rejected; fresh terminal success")
		})
	}
}
