package postgres

import (
	"context"
	"database/sql"
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
		"aws_scan_status.fencing_token < EXCLUDED.fencing_token",
		"aws_scan_status.fencing_token = EXCLUDED.fencing_token",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("StartAWSScan() query missing %q:\n%s", want, query)
		}
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
		if len(exec.args) != 18 && len(exec.args) != 10 {
			t.Fatalf("arg count = %d, want 18 for observe or 10 for commit", len(exec.args))
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
