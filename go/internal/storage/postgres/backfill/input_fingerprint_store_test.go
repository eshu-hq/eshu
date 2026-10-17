// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"
)

type capturedExec struct {
	query string
	args  []any
}

type captureExecutor struct {
	calls []capturedExec
	err   error
}

func (c *captureExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	c.calls = append(c.calls, capturedExec{query: query, args: args})
	return nil, c.err
}

// TestInputFingerprintStoreRecordsOneIdempotentUpsert pins the write shape: one
// upsert keyed on the work item id, the domain fixed to the deployable-unit
// domain, the repo ids trimmed and de-duplicated (never NULL), the fingerprint
// stored verbatim, and recorded_at from the injected clock in UTC.
func TestInputFingerprintStoreRecordsOneIdempotentUpsert(t *testing.T) {
	t.Parallel()

	exec := &captureExecutor{}
	store := NewInputFingerprintStore(exec)
	fixed := time.Date(2026, 10, 16, 12, 0, 0, 0, time.FixedZone("x", 3600))
	store.Now = func() time.Time { return fixed }

	if err := store.RecordDeployableUnitInputFingerprint(
		context.Background(), " wi-1 ", []string{" repo-b", "repo-a", "repo-b", ""}, "fp-1",
	); err != nil {
		t.Fatalf("RecordDeployableUnitInputFingerprint() error = %v", err)
	}
	if len(exec.calls) != 1 || exec.calls[0].query != UpsertInputFingerprintSQL {
		t.Fatalf("calls = %+v, want exactly the upsert", exec.calls)
	}
	if !strings.Contains(UpsertInputFingerprintSQL, "ON CONFLICT (work_item_id) DO UPDATE") {
		t.Fatal("upsert must be keyed on work_item_id so a retry overwrites the one row")
	}
	args := exec.calls[0].args
	if args[0] != "wi-1" || args[1] != "deployable_unit_correlation" || args[3] != "fp-1" {
		t.Fatalf("args = %v, want trimmed id, domain, verbatim fingerprint", args)
	}
	if got := args[4].(time.Time); !got.Equal(fixed) || got.Location() != time.UTC {
		t.Fatalf("recorded_at = %v, want the injected instant in UTC", got)
	}
	repoValue, err := args[2].(driver.Valuer).Value()
	if err != nil || repoValue != `{"repo-b","repo-a"}` {
		t.Fatalf("repo_ids = (%v, %v), want the trimmed, de-duplicated array in first-seen order", repoValue, err)
	}
}

// TestInputFingerprintStoreRefusesIncompleteRows keeps a half-built row out of
// the table: a blank work item id or fingerprint is an error, never a write.
func TestInputFingerprintStoreRefusesIncompleteRows(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ id, fingerprint string }{
		"blank id":          {"  ", "fp"},
		"blank fingerprint": {"wi-1", ""},
	} {
		exec := &captureExecutor{}
		err := NewInputFingerprintStore(exec).RecordDeployableUnitInputFingerprint(
			context.Background(), tc.id, []string{"repo-a"}, tc.fingerprint)
		if err == nil || len(exec.calls) != 0 {
			t.Fatalf("%s: err=%v writes=%d, want an error and no write", name, err, len(exec.calls))
		}
	}
	if err := (InputFingerprintStore{}).RecordDeployableUnitInputFingerprint(
		context.Background(), "wi-1", nil, "fp"); err == nil {
		t.Fatal("a store without a database must refuse to record")
	}
	boom := errors.New("boom")
	err := NewInputFingerprintStore(&captureExecutor{err: boom}).RecordDeployableUnitInputFingerprint(
		context.Background(), "wi-1", nil, "fp")
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the executor failure wrapped", err)
	}
}
