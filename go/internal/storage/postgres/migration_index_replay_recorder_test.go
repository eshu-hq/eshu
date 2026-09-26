// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// This file holds the migration index-replay recorder shared by the live
// replay proofs. It carries no build tag because
// TestServiceMaterializationActiveIndexReplayConvergesLive (#6475) runs in the
// default build under the reducer contention gate; the integration-tagged
// replay proofs (code reachability, identity epoch) use it too.

// migrationIndexReplayRecorder applies bootstrap definitions through the
// production executor while reading the index state on one table either side of
// each statement, so a definition whose effect a later definition undoes is
// still caught even though the state either side of the whole pass matches.
type migrationIndexReplayRecorder struct {
	db      SQLDB
	t       *testing.T
	schema  string
	table   string
	changes []string
	// events carries the same observations as changes in a form a caller can
	// count per index, which is how a test asserts that converging an install
	// costs exactly one build rather than merely ending in the right state.
	events []migrationIndexChange
}

// migrationIndexChange is one index-level effect of one bootstrap definition.
// Kind is "built", "rebuilt", or "dropped".
type migrationIndexChange struct {
	Statement string
	Index     string
	Kind      string
}

// ExecContext runs one definition through the plain executor path.
func (recorder *migrationIndexReplayRecorder) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return recorder.record(ctx, query, func() (sql.Result, error) {
		return recorder.db.ExecContext(ctx, query, args...)
	})
}

// execContextWithLockTimeout runs one definition through the same bounded
// lock-timeout path a real bootstrap uses, so this proof exercises the
// production statement path rather than a simplified one.
func (recorder *migrationIndexReplayRecorder) execContextWithLockTimeout(
	ctx context.Context,
	query string,
	lockTimeout time.Duration,
) (sql.Result, error) {
	return recorder.record(ctx, query, func() (sql.Result, error) {
		return recorder.db.execContextWithLockTimeout(ctx, query, lockTimeout)
	})
}

func (recorder *migrationIndexReplayRecorder) record(
	ctx context.Context,
	query string,
	exec func() (sql.Result, error),
) (sql.Result, error) {
	before := migrationIndexStateLive(ctx, recorder.t, recorder.db.DB, recorder.schema, recorder.table)
	result, err := exec()
	after := migrationIndexStateLive(ctx, recorder.t, recorder.db.DB, recorder.schema, recorder.table)
	statement := migrationStatementSummaryLive(query)
	for name, relfilenode := range after {
		previous, ok := before[name]
		switch {
		case !ok:
			recorder.note(statement, name, "built",
				fmt.Sprintf("%q built index %s", statement, name))
		case previous != relfilenode:
			recorder.note(statement, name, "rebuilt",
				fmt.Sprintf("%q rebuilt index %s (relfilenode %d -> %d)",
					statement, name, previous, relfilenode))
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			recorder.note(statement, name, "dropped",
				fmt.Sprintf("%q dropped index %s", statement, name))
		}
	}
	return result, err
}

// note records one index-level effect in both renderings the callers need: the
// prose line a failure message prints, and the structured event a caller counts.
func (recorder *migrationIndexReplayRecorder) note(statement, index, kind, rendered string) {
	recorder.changes = append(recorder.changes, rendered)
	recorder.events = append(recorder.events, migrationIndexChange{
		Statement: statement,
		Index:     index,
		Kind:      kind,
	})
}

// migrationStatementSummaryLive reduces one migration file to its executable
// statement so a failure names the statement rather than pages of comment.
func migrationStatementSummaryLive(query string) string {
	for _, line := range strings.Split(query, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		return trimmed
	}
	return strings.TrimSpace(query)
}

// migrationIndexStateLive maps every index on one table to its relfilenode,
// which a rebuild changes and a plain reapply does not. The table is a
// parameter because more than one migration family needs this proof: the
// reachability indexes (code_reachability_index_replay_live_test.go), the
// container-image identity epoch index (identity_epoch_index_replay_live_test.go),
// and the service lineage active index
// (service_materialization_scope_replay_live_test.go).
func migrationIndexStateLive(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	schema string,
	table string,
) map[string]int64 {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
SELECT index_class.relname, index_class.relfilenode
FROM pg_index AS index_entry
JOIN pg_class AS index_class ON index_class.oid = index_entry.indexrelid
JOIN pg_class AS table_class ON table_class.oid = index_entry.indrelid
JOIN pg_namespace AS namespace ON namespace.oid = table_class.relnamespace
WHERE namespace.nspname = $1
  AND table_class.relname = $2
`, schema, table)
	if err != nil {
		t.Fatalf("read %s index state: %v", table, err)
	}
	defer func() { _ = rows.Close() }()

	state := map[string]int64{}
	for rows.Next() {
		var name string
		var relfilenode int64
		if err := rows.Scan(&name, &relfilenode); err != nil {
			t.Fatalf("scan %s index state: %v", table, err)
		}
		state[name] = relfilenode
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s index state: %v", table, err)
	}
	return state
}
