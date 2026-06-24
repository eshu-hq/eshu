// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestAWSScanStatusSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := AWSScanStatusSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS aws_scan_status",
		"collector_instance_id TEXT NOT NULL",
		"account_id TEXT NOT NULL",
		"region TEXT NOT NULL",
		"service_kind TEXT NOT NULL",
		"status TEXT NOT NULL",
		"commit_status TEXT NOT NULL",
		"api_call_count INTEGER NOT NULL DEFAULT 0",
		"throttle_count INTEGER NOT NULL DEFAULT 0",
		"budget_exhausted BOOLEAN NOT NULL DEFAULT FALSE",
		"credential_failed BOOLEAN NOT NULL DEFAULT FALSE",
		"PRIMARY KEY (collector_instance_id, account_id, region, service_kind)",
		"aws_scan_status_status_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("AWSScanStatusSchemaSQL() missing %q:\n%s", want, sql)
		}
	}
}

func TestAWSScanStatusStoreUsesFenceGuard(t *testing.T) {
	t.Parallel()

	db := &awsScanStatusTestDB{execResults: []sql.Result{awsCheckpointRowsResult{rowsAffected: 1}}}
	store := NewAWSScanStatusStore(db)
	startedAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	err := store.StartAWSScan(context.Background(), awscloud.ScanStatusStart{
		Boundary:  awsScanStatusBoundary(startedAt),
		StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("StartAWSScan() error = %v, want nil", err)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"INSERT INTO aws_scan_status",
		"ON CONFLICT (collector_instance_id, account_id, region, service_kind) DO UPDATE SET",
		"CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id",
		"aws_scan_status.fencing_token <= EXCLUDED.fencing_token",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("StartAWSScan() query missing %q:\n%s", want, query)
		}
	}
}

func TestAWSScanStatusStoreAllowsNewGenerationAfterTerminalPriorScan(t *testing.T) {
	t.Parallel()

	query := startAWSScanStatusQuery
	for _, want := range []string{
		"aws_scan_status.generation_id <> EXCLUDED.generation_id",
		"aws_scan_status.status IN ('succeeded', 'partial', 'failed', 'credential_failed')",
		"aws_scan_status.commit_status IN ('committed', 'failed')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("StartAWSScan() query missing restart-safe generation handoff %q:\n%s", want, query)
		}
	}
}

func TestAWSScanStatusStoreAllowsNewGenerationAfterTerminalPermissionGap(t *testing.T) {
	t.Parallel()

	query := strings.Join(strings.Fields(startAWSScanStatusQuery), " ")
	for _, want := range []string{
		"aws_scan_status.commit_status = 'pending'",
		"aws_scan_status.failure_class IN ('permission_denied', 'unsupported_permission') AND ( aws_scan_status.last_started_at IS NULL OR aws_scan_status.last_started_at < EXCLUDED.last_started_at )",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("StartAWSScan() query missing permission-gap handoff %q:\n%s", want, query)
		}
	}
}

// TestAWSScanStatusStoreAllowsNewGenerationOverOrphanedRunningRow proves that
// a new workflow generation can claim the per-target slot when the previous
// generation's row was left in a non-terminal state (e.g. the collector died
// between StartAWSScan and ObserveAWSScan). Without this widening, a single
// orphaned 'running'/'pending' row blocks every future generation for that
// (instance, account, region, service_kind) tuple and the collector spins
// stale-fence retries forever — the symptom called out in issue #612.
func TestAWSScanStatusStoreAllowsNewGenerationOverOrphanedRunningRow(t *testing.T) {
	t.Parallel()

	query := startAWSScanStatusQuery
	if !strings.Contains(query, "aws_scan_status.last_started_at < EXCLUDED.last_started_at") {
		t.Fatalf("StartAWSScan() query missing orphan handoff guard 'aws_scan_status.last_started_at < EXCLUDED.last_started_at':\n%s", query)
	}
	if !strings.Contains(query, "aws_scan_status.last_started_at IS NULL") {
		t.Fatalf("StartAWSScan() query missing first-write orphan guard 'aws_scan_status.last_started_at IS NULL':\n%s", query)
	}
}

// TestAWSScanStatusStoreReturnsTypedStaleFenceError pins the typed error
// returned when a status mutation is rejected by row count. Issue #612: the
// awsruntime classifier must be able to detect this with errors.Is so it can
// route the failed claim to terminal instead of looping it back through the
// retryable queue.
func TestAWSScanStatusStoreReturnsTypedStaleFenceError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		do   func(t *testing.T, store AWSScanStatusStore, boundary awscloud.Boundary, now time.Time) error
	}{
		{
			name: "start",
			do: func(_ *testing.T, store AWSScanStatusStore, boundary awscloud.Boundary, now time.Time) error {
				return store.StartAWSScan(context.Background(), awscloud.ScanStatusStart{
					Boundary:  boundary,
					StartedAt: now,
				})
			},
		},
		{
			name: "observe",
			do: func(_ *testing.T, store AWSScanStatusStore, boundary awscloud.Boundary, now time.Time) error {
				return store.ObserveAWSScan(context.Background(), awscloud.ScanStatusObservation{
					Boundary:   boundary,
					Status:     awscloud.ScanStatusFailed,
					ObservedAt: now,
				})
			},
		},
		{
			name: "commit",
			do: func(_ *testing.T, store AWSScanStatusStore, boundary awscloud.Boundary, now time.Time) error {
				return store.CommitAWSScan(context.Background(), awscloud.ScanStatusCommit{
					Boundary:     boundary,
					CommitStatus: awscloud.ScanCommitFailed,
					CompletedAt:  now,
				})
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := &awsScanStatusTestDB{execResults: []sql.Result{awsCheckpointRowsResult{rowsAffected: 0}}}
			store := NewAWSScanStatusStore(db)
			now := time.Date(2026, 5, 24, 17, 0, 0, 0, time.UTC)

			err := tc.do(t, store, awsScanStatusBoundary(now), now)
			if err == nil {
				t.Fatalf("%s returned nil, want stale fence error", tc.name)
			}
			if !errors.Is(err, awscloud.ErrScanStatusStaleFence) {
				t.Fatalf("%s err = %v, want errors.Is awscloud.ErrScanStatusStaleFence", tc.name, err)
			}
		})
	}
}

func TestAWSScanStatusStoreUsesExactFenceForObserveAndCommit(t *testing.T) {
	t.Parallel()

	db := &awsScanStatusTestDB{
		execResults: []sql.Result{
			awsCheckpointRowsResult{rowsAffected: 1},
			awsCheckpointRowsResult{rowsAffected: 1},
		},
	}
	store := NewAWSScanStatusStore(db)
	observedAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	boundary := awsScanStatusBoundary(observedAt)
	if err := store.ObserveAWSScan(context.Background(), awscloud.ScanStatusObservation{
		Boundary:   boundary,
		Status:     awscloud.ScanStatusSucceeded,
		ObservedAt: observedAt,
	}); err != nil {
		t.Fatalf("ObserveAWSScan() error = %v, want nil", err)
	}
	if err := store.CommitAWSScan(context.Background(), awscloud.ScanStatusCommit{
		Boundary:     boundary,
		CommitStatus: awscloud.ScanCommitCommitted,
		CompletedAt:  observedAt,
	}); err != nil {
		t.Fatalf("CommitAWSScan() error = %v, want nil", err)
	}

	for _, exec := range db.execs {
		if strings.Contains(exec.query, "fencing_token <=") {
			t.Fatalf("query uses range fence guard, want exact fence:\n%s", exec.query)
		}
		if !strings.Contains(exec.query, "AND fencing_token = $6") {
			t.Fatalf("query missing exact fence guard:\n%s", exec.query)
		}
		assertPostgresPlaceholdersMatchArgs(t, exec.query, len(exec.args))
		if len(exec.args) != 18 && len(exec.args) != 10 {
			t.Fatalf("arg count = %d, want 18 for observe or 10 for commit", len(exec.args))
		}
	}
}

func TestAWSScanStatusStoreClearsCommitFailureAfterSuccessfulCommit(t *testing.T) {
	t.Parallel()

	query := strings.Join(strings.Fields(commitAWSScanStatusQuery), " ")
	for _, want := range []string{
		"failure_class = CASE WHEN $7 = 'committed' AND status = 'succeeded' THEN '' WHEN $8 = '' THEN failure_class ELSE $8 END",
		"failure_message = CASE WHEN $7 = 'committed' AND status = 'succeeded' THEN '' WHEN $9 = '' THEN failure_message ELSE $9 END",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("CommitAWSScan() query missing successful-commit cleanup %q:\n%s", want, query)
		}
	}
}

func assertPostgresPlaceholdersMatchArgs(t *testing.T, query string, argCount int) {
	t.Helper()

	matches := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(query, -1)
	seen := make(map[int]bool, len(matches))
	maxPlaceholder := 0
	for _, match := range matches {
		placeholder, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse placeholder %q: %v", match[0], err)
		}
		seen[placeholder] = true
		if placeholder > maxPlaceholder {
			maxPlaceholder = placeholder
		}
	}
	if maxPlaceholder != argCount {
		t.Fatalf("query max placeholder = $%d, args = %d:\n%s", maxPlaceholder, argCount, query)
	}
	for placeholder := 1; placeholder <= maxPlaceholder; placeholder++ {
		if !seen[placeholder] {
			t.Fatalf("query skips placeholder $%d:\n%s", placeholder, query)
		}
	}
}

func awsScanStatusBoundary(observedAt time.Time) awscloud.Boundary {
	return awscloud.Boundary{
		CollectorInstanceID: "aws-prod",
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceECR,
		ScopeID:             "aws:123456789012:us-east-1:ecr",
		GenerationID:        "generation-1",
		FencingToken:        4,
		ObservedAt:          observedAt,
	}
}

type awsScanStatusTestDB struct {
	execs       []awsCheckpointExec
	execResults []sql.Result
}

func (db *awsScanStatusTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execs = append(db.execs, awsCheckpointExec{query: query, args: args})
	if len(db.execResults) == 0 {
		return awsCheckpointRowsResult{rowsAffected: 1}, nil
	}
	result := db.execResults[0]
	db.execResults = db.execResults[1:]
	return result, nil
}

func (db *awsScanStatusTestDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, sql.ErrNoRows
}
