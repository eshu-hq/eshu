// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// execIndexContaining returns the index of the first exec whose statement and
// arguments contain fragment, or -1.
func execIndexContaining(execs []fakeExecCall, fragment string) int {
	for i, call := range execs {
		if strings.Contains(call.query+" "+fmt.Sprint(call.args...), fragment) {
			return i
		}
	}
	return -1
}

// TestPurgeRetiredBoundsTheDeriveLockWait pins the #10820 wait budget: the purge
// sets a 10 s transaction-local lock_timeout before it takes the two derive
// advisory locks and restores the prior value once both are held, so the later
// deletes keep today's wait behavior and the advisory wait is bounded.
func TestPurgeRetiredBoundsTheDeriveLockWait(t *testing.T) {
	t.Parallel()

	_, execs := purgeExecOrder(t)
	bound := -1
	for i, call := range execs {
		if strings.Contains(call.query, "lock_timeout") && len(call.args) == 1 && call.args[0] == "10000ms" {
			bound = i
			break
		}
	}
	if bound < 0 {
		t.Fatalf("no exec sets a 10000ms lock_timeout before the derive locks:\n%v", execs)
	}
	infra := execIndexContaining(execs, "infra_resource_entities:")
	config := execIndexContaining(execs, "config_artifact_paths:")
	restore := -1
	for i, call := range execs {
		if i > config && strings.Contains(call.query, "lock_timeout") && len(call.args) == 0 {
			restore = i
			break
		}
	}
	contentDelete := execIndexContaining(execs, "DELETE FROM content_files")
	if bound >= infra || infra >= config || config >= restore || restore >= contentDelete {
		t.Fatalf("want set budget (%d) < infra lock (%d) < config lock (%d) < restore (%d) < first content delete (%d)",
			bound, infra, config, restore, contentDelete)
	}
}

// TestPurgeRetiredReportsDeriveLockBusy proves a lock_timeout (55P03) on either
// derive lock is a designed blocked exit: nothing written, the transaction
// rolled back, and a WARN log that names the repository and the reason.
func TestPurgeRetiredReportsDeriveLockBusy(t *testing.T) {
	t.Parallel()

	for _, lockKey := range []string{"infra_resource_entities:", "config_artifact_paths:"} {
		lockKey := lockKey
		t.Run(lockKey, func(t *testing.T) {
			t.Parallel()
			_, execs := purgeExecOrder(t)
			idx := execIndexContaining(execs, lockKey)
			if idx < 0 {
				t.Fatalf("no exec takes %s", lockKey)
			}
			database, tx := stagePurgeScriptedDB()
			tx.execErrors = map[int]error{idx: &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}}
			var logs bytes.Buffer
			purger := NewRetirementPurger(database, database)
			purger.Options.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			result, err := purger.PurgeRetired(context.Background(), purgeTestRequest())
			if err != nil {
				t.Fatalf("PurgeRetired: %v (a lock timeout is a blocked exit, not an error)", err)
			}
			if len(result.Completed) != 0 || len(result.Blocked) != 1 || result.Blocked[0].Reason != "derive_lock_busy" {
				t.Fatalf("result = %+v, want one derive_lock_busy block", result)
			}
			if tx.committed || !tx.rolledBack {
				t.Fatalf("committed=%v rolledBack=%v, want a rollback with nothing committed", tx.committed, tx.rolledBack)
			}
			for _, call := range tx.execs {
				if strings.Contains(call.query, "DELETE FROM") || strings.Contains(call.query, "SET state = 'complete'") {
					t.Fatalf("a write ran before the lock timeout: %s", call.query)
				}
			}
			for _, want := range []string{"derive_lock_busy", "PARTITION:purge-repo", "level=WARN"} {
				if !strings.Contains(logs.String(), want) {
					t.Errorf("WARN log lacks %q:\n%s", want, logs.String())
				}
			}
		})
	}
}

// TestPurgeRetiredDoesNotCallOtherLockErrorsBusy proves only a lock timeout
// (55P03) is the designed derive_lock_busy exit. Any other error on a derive
// lock (a deadlock, a cancelled statement, a plain failure) is a failed purge:
// it returns an error, reports no blocked repository, rolls back, and logs no
// derive_lock_busy line, so a real fault is never filed as ordinary contention.
func TestPurgeRetiredDoesNotCallOtherLockErrorsBusy(t *testing.T) {
	t.Parallel()

	failures := map[string]error{
		"deadlock_detected":  &pgconn.PgError{Code: "40P01", Message: "deadlock detected"},
		"query_canceled":     &pgconn.PgError{Code: "57014", Message: "canceling statement due to user request"},
		"non_postgres_error": errors.New("connection reset by peer"),
	}
	for _, lockKey := range []string{"infra_resource_entities:", "config_artifact_paths:"} {
		for name, failure := range failures {
			lockKey, name, failure := lockKey, name, failure
			t.Run(lockKey+"/"+name, func(t *testing.T) {
				t.Parallel()
				_, execs := purgeExecOrder(t)
				idx := execIndexContaining(execs, lockKey)
				if idx < 0 {
					t.Fatalf("no exec takes %s", lockKey)
				}
				database, tx := stagePurgeScriptedDB()
				tx.execErrors = map[int]error{idx: failure}
				var logs bytes.Buffer
				purger := NewRetirementPurger(database, database)
				purger.Options.Logger = slog.New(slog.NewTextHandler(&logs, nil))
				result, err := purger.PurgeRetired(context.Background(), purgeTestRequest())
				if err == nil {
					t.Fatalf("PurgeRetired = %+v, want the %s failure returned as an error", result, name)
				}
				if !errors.Is(err, failure) {
					t.Fatalf("error = %v, want it to wrap %v", err, failure)
				}
				if len(result.Blocked) != 0 || len(result.Completed) != 0 {
					t.Fatalf("result = %+v, want no blocked or completed repository on a failed purge", result)
				}
				if tx.committed || !tx.rolledBack {
					t.Fatalf("committed=%v rolledBack=%v, want a rollback with nothing committed", tx.committed, tx.rolledBack)
				}
				if strings.Contains(logs.String(), "derive_lock_busy") {
					t.Fatalf("a non-timeout lock error was logged as derive_lock_busy:\n%s", logs.String())
				}
			})
		}
	}
}
