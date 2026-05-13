package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestReducerQueueFailClassifiesGraphWriteTimeoutAfterAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	intent := reducer.Intent{
		IntentID:     "intent-1",
		AttemptCount: 3,
	}
	cause := fmt.Errorf("semantic materialization: %w", sourcecypher.GraphWriteTimeoutError{
		Operation:   "neo4j execute timed out",
		Timeout:     30 * time.Second,
		TimeoutHint: "ESHU_CANONICAL_WRITE_TIMEOUT",
		Summary:     "semantic label=Annotation rows=10",
		Cause:       context.DeadlineExceeded,
	})

	if err := queue.Fail(context.Background(), intent, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'dead_letter'") {
		t.Fatalf("exhausted timeout should dead-letter, query:\n%s", db.execs[0].query)
	}
	if got, want := db.execs[0].args[1], "graph_write_timeout"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], "semantic label=Annotation rows=10"; got != want {
		t.Fatalf("failure details = %v, want %v", got, want)
	}
}
func TestReducerQueueAckAndFailUpdateClaimedWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	intent := reducer.Intent{IntentID: "intent-1"}
	if err := queue.Ack(context.Background(), intent, reducer.Result{}); err != nil {
		t.Fatalf("Ack() error = %v, want nil", err)
	}
	if err := queue.Fail(context.Background(), intent, errors.New("boom")); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
}

func TestBootstrapWorkItemSchemaIncludesPayloadAndLeaseOwner(t *testing.T) {
	t.Parallel()

	var workItemSQL string
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_work_items" {
			workItemSQL = def.SQL
			break
		}
	}
	if workItemSQL == "" {
		t.Fatal("fact_work_items definition missing")
	}
	for _, want := range []string{
		"lease_owner TEXT NULL",
		"conflict_domain TEXT NOT NULL DEFAULT 'scope'",
		"conflict_key TEXT NULL",
		"payload JSONB NOT NULL DEFAULT '{}'::jsonb",
		"ADD COLUMN IF NOT EXISTS conflict_domain TEXT NOT NULL DEFAULT 'scope'",
		"ADD COLUMN IF NOT EXISTS conflict_key TEXT NULL",
		"fact_work_items_reducer_conflict_claim_idx",
		"COALESCE(conflict_key, scope_id)",
	} {
		if !strings.Contains(workItemSQL, want) {
			t.Fatalf("work item SQL missing %q", want)
		}
	}
}

type fakeExecQueryer struct {
	// mu guards every slice in this struct so the fake is safe to use from
	// concurrent ExecContext callers. The ContentWriter's parallel-batch
	// path (content_writer_batch.go) issues concurrent ExecContext calls per
	// upsert batch; the prior unguarded slice appends were racy under -race
	// and produced flaky ordering in tests that index db.execs.
	mu             sync.Mutex
	execs          []fakeExecCall
	execErrors     []error
	execResults    []sql.Result
	queries        []fakeQueryCall
	queryResponses []queueFakeRows
}

type fakeExecCall struct {
	query string
	args  []any
}

type fakeQueryCall struct {
	query string
	args  []any
}

func (f *fakeExecQueryer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, fakeExecCall{query: query, args: args})
	if len(f.execErrors) > 0 {
		err := f.execErrors[0]
		f.execErrors = f.execErrors[1:]
		return nil, err
	}
	if len(f.execResults) > 0 {
		result := f.execResults[0]
		f.execResults = f.execResults[1:]
		return result, nil
	}
	return fakeResult{}, nil
}

func (f *fakeExecQueryer) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, fakeQueryCall{query: query, args: args})
	if len(f.queryResponses) == 0 {
		if isWorkflowCoordinatorStatusQuery(query) {
			return &queueFakeRows{}, nil
		}
		return nil, fmt.Errorf("unexpected query: %s", query)
	}

	rows := f.queryResponses[0]
	f.queryResponses = f.queryResponses[1:]
	if rows.err != nil {
		return nil, rows.err
	}

	return &rows, nil
}

func isWorkflowCoordinatorStatusQuery(query string) bool {
	return strings.Contains(query, "FROM collector_instances") ||
		strings.Contains(query, "FROM workflow_runs") ||
		strings.Contains(query, "FROM workflow_work_items") ||
		strings.Contains(query, "JOIN workflow_work_items") ||
		strings.Contains(query, "FROM workflow_run_completeness") ||
		strings.Contains(query, "FROM workflow_claims") ||
		isTerraformStateAdminQuery(query)
}

// isTerraformStateAdminQuery flags the optional tfstate admin status queries
// so existing fake queryers in this package can return empty rows for runtimes
// whose fixtures do not exercise tfstate evidence.
func isTerraformStateAdminQuery(query string) bool {
	return strings.Contains(query, "FROM ranked_generations") ||
		strings.Contains(query, "FROM warning_rows")
}

type queueFakeRows struct {
	rows  [][]any
	err   error
	index int
}

func (r *queueFakeRows) Next() bool {
	return r.index < len(r.rows)
}

func (r *queueFakeRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("scan called without row")
	}
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}

	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *bool:
			value, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want bool", i, row[i])
			}
			*target = value
		case *[]byte:
			value, ok := row[i].([]byte)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []byte", i, row[i])
			}
			*target = value
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want time.Time", i, row[i])
			}
			*target = value
		case *int:
			value, ok := row[i].(int)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int", i, row[i])
			}
			*target = value
		case *int64:
			value, ok := row[i].(int64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int64", i, row[i])
			}
			*target = value
		case *sql.NullString:
			value, ok := row[i].(sql.NullString)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullString", i, row[i])
			}
			*target = value
		case *sql.NullBool:
			value, ok := row[i].(sql.NullBool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullBool", i, row[i])
			}
			*target = value
		case *sql.NullInt64:
			value, ok := row[i].(sql.NullInt64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullInt64", i, row[i])
			}
			*target = value
		case *sql.NullTime:
			value, ok := row[i].(sql.NullTime)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullTime", i, row[i])
			}
			*target = value
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}

	r.index++
	return nil
}

func (r *queueFakeRows) Err() error { return nil }

func (r *queueFakeRows) Close() error { return nil }

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }

func (fakeResult) RowsAffected() (int64, error) { return 1, nil }

type retryableReducerTestError struct {
	message string
}

func (e retryableReducerTestError) Error() string {
	return e.message
}

func (e retryableReducerTestError) Retryable() bool {
	return true
}
