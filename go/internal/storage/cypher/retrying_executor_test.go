package cypher

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type failingExecutor struct {
	calls   atomic.Int32
	failFor int    // fail this many times then succeed
	errMsg  string // error message to return
}

func (f *failingExecutor) Execute(_ context.Context, _ Statement) error {
	n := int(f.calls.Add(1))
	if n <= f.failFor {
		return errors.New(f.errMsg)
	}
	return nil
}

// failingGroupExecutor implements both Executor and GroupExecutor and fails
// ExecuteGroup the first failFor invocations with errMsg, then succeeds. It
// exists to exercise RetryingExecutor.ExecuteGroup retry classification
// against shapes such as NornicDB commit-time UNIQUE conflicts on MERGE.
type failingGroupExecutor struct {
	calls   atomic.Int32
	failFor int
	errMsg  string
}

func (f *failingGroupExecutor) Execute(_ context.Context, _ Statement) error {
	// Not used in ExecuteGroup retry tests; behave as success.
	return nil
}

func (f *failingGroupExecutor) ExecuteGroup(_ context.Context, _ []Statement) error {
	n := int(f.calls.Add(1))
	if n <= f.failFor {
		return errors.New(f.errMsg)
	}
	return nil
}

// groupCapableExecutor implements both Executor and GroupExecutor for testing.
type groupCapableExecutor struct {
	executeCalls      atomic.Int32
	executeGroupCalls atomic.Int32
	groupStmts        []Statement
	groupErr          error
}

func (g *groupCapableExecutor) Execute(_ context.Context, _ Statement) error {
	g.executeCalls.Add(1)
	return nil
}

func (g *groupCapableExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	g.executeGroupCalls.Add(1)
	g.groupStmts = stmts
	return g.groupErr
}

func TestRetryingExecutorRetriesOnDeadlock(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{
		failFor: 2,
		errMsg:  "Neo4jError: Neo.TransientError.Transaction.DeadlockDetected (deadlock cycle)",
	}

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	err := r.Execute(context.Background(), Statement{Operation: OperationCanonicalUpsert})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if got := int(inner.calls.Load()); got != 3 {
		t.Errorf("calls = %d, want 3 (2 failures + 1 success)", got)
	}
}

func TestRetryingExecutorDoesNotRetryPermanentErrors(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{
		failFor: 10,
		errMsg:  "Neo4jError: Neo.ClientError.Schema.ConstraintValidationFailed",
	}

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	err := r.Execute(context.Background(), Statement{Operation: OperationCanonicalUpsert})
	if err == nil {
		t.Fatal("expected error for permanent failure")
	}
	if got := int(inner.calls.Load()); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry for permanent error)", got)
	}
}

func TestRetryingExecutorRetriesNornicDBMergeUniqueConflict(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{
		failFor: 1,
		errMsg: "Neo4jError: Neo.ClientError.Statement.SyntaxError " +
			"(failed to commit implicit transaction: constraint violation: " +
			"Constraint violation (UNIQUE on Platform.[id]): Node with id=platform:kubernetes:none:prod:prod:none already exists)",
	}

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	err := r.Execute(context.Background(), Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (p:Platform {id: row.platform_id}) SET p.name = row.platform_name",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil after retry", err)
	}
	if got, want := int(inner.calls.Load()), 2; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
}

func TestRetryingExecutorDoesNotRetryNornicDBUniqueConflictWithoutMerge(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{
		failFor: 10,
		errMsg: "Neo4jError: Neo.ClientError.Statement.SyntaxError " +
			"(failed to commit implicit transaction: constraint violation: " +
			"Constraint violation (UNIQUE on Platform.[id]): Node with id=platform:kubernetes:none:prod:prod:none already exists)",
	}

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	err := r.Execute(context.Background(), Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    "CREATE (p:Platform {id: $platform_id})",
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if got, want := int(inner.calls.Load()), 1; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
}

func TestRetryingExecutorExhaustsRetries(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{
		failFor: 10,
		errMsg:  "Neo4jError: Neo.TransientError.Transaction.DeadlockDetected",
	}

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 2,
		BaseDelay:  1 * time.Millisecond,
	}

	err := r.Execute(context.Background(), Statement{Operation: OperationCanonicalUpsert})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("error retryable = false, want true")
	}
	// 1 initial + 2 retries = 3 calls
	if got := int(inner.calls.Load()); got != 3 {
		t.Errorf("calls = %d, want 3 (initial + 2 retries)", got)
	}
}

func TestRetryingExecutorPassesThroughOnSuccess(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{failFor: 0} // never fails

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	err := r.Execute(context.Background(), Statement{Operation: OperationCanonicalUpsert})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := int(inner.calls.Load()); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestRetryingExecutorRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{
		failFor: 10,
		errMsg:  "Neo4jError: Neo.TransientError.Transaction.DeadlockDetected",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 5,
		BaseDelay:  1 * time.Millisecond,
	}

	err := r.Execute(ctx, Statement{Operation: OperationCanonicalUpsert})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

// TestRetryingExecutorRetriesNornicDBMergeUniqueConflictV1045Format covers
// the production error shape returned by timothyswt/nornicdb-amd64-cpu:v1.0.45
// at commit time, which differs from the older "failed to commit implicit
// transaction" wrapping the classifier was originally written against. The
// v1.0.45 binary surfaces commit-time UNIQUE violations as a Neo4jError with
// code Neo.ClientError.Transaction.TransactionCommitFailed and body
// "commit failed: constraint violation:...". This test pins the classifier
// to keep both wrappings retryable so the canonical MERGE writer remains
// idempotent under concurrent commit on the same uid across NornicDB
// versions; if Eshu only recognized the older shape, concurrent MERGE
// would surface as a non-retryable projector failure on the pinned binary
// despite being safe to retry by construction.
func TestRetryingExecutorRetriesNornicDBMergeUniqueConflictV1045Format(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{
		failFor: 1,
		errMsg: "Neo4jError: Neo.ClientError.Transaction.TransactionCommitFailed " +
			"(commit failed: constraint violation: " +
			"Constraint violation (UNIQUE on TerraformResource.[uid]): " +
			"Node with uid=1b579c9b2e26be17c853767e13c7c747f81f8d25524ee052af40db54eabe8821 " +
			"already exists (nodeID: 508af30f-fb36-4012-a977-61d9d87dd556))",
	}

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	err := r.Execute(context.Background(), Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (r:TerraformResource {uid: row.uid}) SET r.name = row.address",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil after retry", err)
	}
	if got, want := int(inner.calls.Load()), 2; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
}

// TestRetryingExecutorExecuteGroupRetriesOnCommitTimeUniqueConflict pins that
// phase-group writes — the only canonical projection path used by the
// PhaseGroupExecutor wiring (go/cmd/bootstrap-index/nornicdb_wiring.go,
// go/cmd/ingester/wiring_nornicdb_phase_group.go) — retry on a commit-time
// UNIQUE violation when every statement in the group is MERGE-shaped.
// Without this retry, concurrent canonical writers on the same uid surface
// the race as a projection_failure even though MERGE re-execution is
// idempotent. Worker-knob serialization (ESHU_PROJECTION_WORKERS=1, stopping
// a concurrent writer) is not an acceptable fix per project rule
// "Serialization Is Not A Fix" — the design must absorb the race in the
// retry layer.
func TestRetryingExecutorExecuteGroupRetriesOnCommitTimeUniqueConflict(t *testing.T) {
	t.Parallel()

	inner := &failingGroupExecutor{
		failFor: 1,
		errMsg: "phase-group chunk 1/1 (statements 1-1 of 1, size=1, " +
			"duration=1.814001ms, first_statement=\"label=TerraformResource rows=1\"): " +
			"Neo4jError: Neo.ClientError.Transaction.TransactionCommitFailed " +
			"(commit failed: constraint violation: " +
			"Constraint violation (UNIQUE on TerraformResource.[uid]): " +
			"Node with uid=1b579c9b already exists (nodeID: 508af30f))",
	}

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	stmts := []Statement{
		{
			Operation: OperationCanonicalUpsert,
			Cypher:    "UNWIND $rows AS row MERGE (r:TerraformResource {uid: row.uid}) SET r.name = row.address",
		},
	}

	err := r.ExecuteGroup(context.Background(), stmts)
	if err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil after retry", err)
	}
	if got, want := int(inner.calls.Load()), 2; got != want {
		t.Fatalf("calls = %d, want %d (1 failure + 1 success)", got, want)
	}
}

// TestRetryingExecutorExecuteGroupDoesNotRetryNonMergeStatements verifies the
// retry path stays narrow: a group that mixes a non-MERGE statement with a
// MERGE statement is NOT retried on commit-time UNIQUE violation, because
// re-executing the non-MERGE statement is not idempotent. This guards
// against the retry loop double-applying CREATE/DELETE/SET-only patterns
// when a future writer adds a non-MERGE statement to a phase group.
func TestRetryingExecutorExecuteGroupDoesNotRetryNonMergeStatements(t *testing.T) {
	t.Parallel()

	inner := &failingGroupExecutor{
		failFor: 10,
		errMsg: "Neo4jError: Neo.ClientError.Transaction.TransactionCommitFailed " +
			"(commit failed: constraint violation: " +
			"Constraint violation (UNIQUE on TerraformResource.[uid]): " +
			"Node with uid=abc already exists)",
	}

	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	// Mix MERGE with a non-MERGE statement; retry must NOT fire.
	stmts := []Statement{
		{
			Operation: OperationCanonicalUpsert,
			Cypher:    "UNWIND $rows AS row MERGE (r:TerraformResource {uid: row.uid})",
		},
		{
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (d:Deleted {uid: $uid}) DETACH DELETE d",
		},
	}

	err := r.ExecuteGroup(context.Background(), stmts)
	if err == nil {
		t.Fatal("ExecuteGroup() error = nil, want non-nil (no retry for mixed group)")
	}
	if got, want := int(inner.calls.Load()), 1; got != want {
		t.Fatalf("calls = %d, want %d (no retry attempted)", got, want)
	}
}

func TestRetryingExecutorForwardsExecuteGroup(t *testing.T) {
	t.Parallel()

	inner := &groupCapableExecutor{}
	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	stmts := []Statement{
		{Operation: OperationCanonicalRetract, Cypher: "MATCH (d) DETACH DELETE d"},
		{Operation: OperationCanonicalUpsert, Cypher: "MERGE (f:File {path: $path})"},
	}

	err := r.ExecuteGroup(context.Background(), stmts)
	if err != nil {
		t.Fatalf("ExecuteGroup() error = %v", err)
	}

	if got := int(inner.executeGroupCalls.Load()); got != 1 {
		t.Errorf("executeGroupCalls = %d, want 1", got)
	}
	if got := int(inner.executeCalls.Load()); got != 0 {
		t.Errorf("executeCalls = %d, want 0 (should not fall back to Execute)", got)
	}
	if len(inner.groupStmts) != 2 {
		t.Errorf("forwarded stmts = %d, want 2", len(inner.groupStmts))
	}
}

func TestRetryingExecutorExecuteGroupErrorsWithoutGroupExecutor(t *testing.T) {
	t.Parallel()

	inner := &failingExecutor{failFor: 0} // only implements Executor, not GroupExecutor
	r := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
	}

	err := r.ExecuteGroup(context.Background(), []Statement{{Cypher: "test"}})
	if err == nil {
		t.Fatal("expected error when Inner does not implement GroupExecutor")
	}
}

func TestIsTransientNeo4jError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"deadlock", errors.New("Neo.TransientError.Transaction.DeadlockDetected"), true},
		{"transient generic", errors.New("something TransientError something"), true},
		{"lock client", errors.New("LockClient timeout"), true},
		{"nornicdb optimistic edge conflict", errors.New("failed to commit implicit transaction: conflict: edge nornic:abc changed after transaction start"), true},
		{"nornicdb optimistic node conflict", errors.New("failed to commit implicit transaction: conflict: node nornic:abc changed after transaction start"), true},
		{"constraint violation", errors.New("Neo.ClientError.Schema.ConstraintValidationFailed"), false},
		{"generic error", errors.New("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isTransientNeo4jError(tt.err)
			if got != tt.expected {
				t.Errorf("isTransientNeo4jError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
