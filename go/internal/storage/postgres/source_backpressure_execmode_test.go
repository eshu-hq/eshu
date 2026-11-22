// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// argsRecordingDB records the argument list of every QueryContext call and
// answers every query with no rows.
type argsRecordingDB struct {
	sourceBackpressureFakeDB
	args [][]any
}

func (f *argsRecordingDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	f.args = append(f.args, args)
	return f.sourceBackpressureFakeDB.QueryContext(ctx, query, args...)
}

// TestSourceSignalReadsRunUnprepared pins the #11187 R4-3 residual fix. pgx's
// default exec mode caches prepared statements, and under plan_cache_mode=auto a
// generic plan made while reducer_waiting_items is analyzed empty keeps its
// sequential scan of the wait table after the table fills, until a statistics
// update forces a re-plan (measured on the 10x seed: the bounded probe went from
// about 95 ms to 160 ms at 1,000 wait rows and 3.9 s at 50,000, the grouped
// query from about 300 ms to 19 s). Every read of the claimable signal therefore
// passes pgx.QueryExecModeExec as its first argument, so each run plans with the
// current wait-table size; placeholders stay $1.. because pgx consumes the mode
// before binding.
func TestSourceSignalReadsRunUnprepared(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	cfg := sourceBackpressureTestConfig()
	for name, run := range map[string]func(*argsRecordingDB) error{
		"bounded consult probe": func(f *argsRecordingDB) error {
			_, err := NewWorkflowControlStore(f).sourceClaimableSignalForSource(context.Background(), cfg, "git", now)
			return err
		},
		"grouped store read": func(f *argsRecordingDB) error {
			_, err := NewWorkflowControlStore(f).sourceClaimableSignals(context.Background(), cfg, now)
			return err
		},
		"status publication read": func(f *argsRecordingDB) error {
			_, err := readSourceClaimableSignals(context.Background(), f, now, cfg)
			return err
		},
	} {
		fake := &argsRecordingDB{}
		if err := run(fake); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(fake.args) != 1 {
			t.Fatalf("%s: ran %d queries, want 1", name, len(fake.args))
		}
		args := fake.args[0]
		if len(args) == 0 || args[0] != pgx.QueryExecModeExec {
			t.Fatalf("%s: first argument = %v, want pgx.QueryExecModeExec so the signal is planned per run", name, args)
		}
		if _, ok := args[1].(time.Time); !ok {
			t.Fatalf("%s: second argument = %T, want the $1 timestamp after the mode", name, args[1])
		}
	}
}
