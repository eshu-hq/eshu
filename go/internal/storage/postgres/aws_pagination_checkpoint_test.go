package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/checkpoint"
)

func TestAWSPaginationCheckpointSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := AWSPaginationCheckpointSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS aws_scan_pagination_checkpoints",
		"collector_instance_id TEXT NOT NULL",
		"account_id TEXT NOT NULL",
		"region TEXT NOT NULL",
		"service_kind TEXT NOT NULL",
		"resource_parent TEXT NOT NULL DEFAULT ''",
		"operation TEXT NOT NULL",
		"generation_id TEXT NOT NULL",
		"fencing_token BIGINT NOT NULL",
		"page_token TEXT NOT NULL DEFAULT ''",
		"page_number INTEGER NOT NULL DEFAULT 0",
		"payload JSONB NOT NULL DEFAULT '{}'::jsonb",
		"PRIMARY KEY (collector_instance_id, account_id, region, service_kind, resource_parent, operation)",
		"aws_scan_pagination_checkpoints_scope_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("AWSPaginationCheckpointSchemaSQL() missing %q:\n%s", want, sql)
		}
	}
}

func TestAWSPaginationCheckpointStoreSaveRejectsOlderFence(t *testing.T) {
	t.Parallel()

	db := &awsCheckpointStoreTestDB{execResults: []sql.Result{awsCheckpointRowsResult{rowsAffected: 0}}}
	store := NewAWSPaginationCheckpointStore(db)
	err := store.Save(context.Background(), checkpoint.Checkpoint{
		Key:        testAWSCheckpointKey(),
		PageToken:  "token-1",
		PageNumber: 2,
		UpdatedAt:  time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, checkpoint.ErrStaleFence) {
		t.Fatalf("Save() error = %v, want ErrStaleFence", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	for _, want := range []string{
		"ON CONFLICT (collector_instance_id, account_id, region, service_kind, resource_parent, operation) DO UPDATE",
		"WHERE aws_scan_pagination_checkpoints.fencing_token <= EXCLUDED.fencing_token",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Save() query missing %q:\n%s", want, query)
		}
	}
}

func TestAWSPaginationCheckpointStoreExpireStaleScopesByGeneration(t *testing.T) {
	t.Parallel()

	db := &awsCheckpointStoreTestDB{execResults: []sql.Result{awsCheckpointRowsResult{rowsAffected: 3}}}
	store := NewAWSPaginationCheckpointStore(db)
	expired, err := store.ExpireStale(context.Background(), testAWSCheckpointScope())
	if err != nil {
		t.Fatalf("ExpireStale() error = %v, want nil", err)
	}
	if expired != 3 {
		t.Fatalf("ExpireStale() = %d, want 3", expired)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"DELETE FROM aws_scan_pagination_checkpoints",
		"collector_instance_id = $1",
		"account_id = $2",
		"region = $3",
		"service_kind = $4",
		"generation_id <> $5",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ExpireStale() query missing %q:\n%s", want, query)
		}
	}
}

func testAWSCheckpointKey() checkpoint.Key {
	return checkpoint.Key{
		Scope: checkpoint.Scope{
			CollectorInstanceID: "aws-prod",
			AccountID:           "123456789012",
			Region:              "us-east-1",
			ServiceKind:         awscloud.ServiceECR,
			GenerationID:        "generation-1",
			FencingToken:        4,
		},
		ResourceParent: "arn:aws:ecr:us-east-1:123456789012:repository/team/api",
		Operation:      "DescribeImages",
	}
}

func testAWSCheckpointScope() checkpoint.Scope {
	return testAWSCheckpointKey().Scope
}

type awsCheckpointStoreTestDB struct {
	execs       []awsCheckpointExec
	execResults []sql.Result
}

func (db *awsCheckpointStoreTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execs = append(db.execs, awsCheckpointExec{query: query, args: args})
	if len(db.execResults) == 0 {
		return awsCheckpointRowsResult{rowsAffected: 1}, nil
	}
	result := db.execResults[0]
	db.execResults = db.execResults[1:]
	return result, nil
}

func (db *awsCheckpointStoreTestDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("unexpected QueryContext")
}

type awsCheckpointExec struct {
	query string
	args  []any
}

type awsCheckpointRowsResult struct {
	rowsAffected int64
}

func (r awsCheckpointRowsResult) LastInsertId() (int64, error) { return 0, nil }
func (r awsCheckpointRowsResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }
