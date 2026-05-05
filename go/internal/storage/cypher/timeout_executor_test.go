package cypher

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTimeoutExecutorCancelsLongRunningExecute(t *testing.T) {
	t.Parallel()

	executor := TimeoutExecutor{
		Inner:   contextBlockingExecutor{},
		Timeout: 10 * time.Millisecond,
	}

	err := executor.Execute(context.Background(), Statement{Cypher: "RETURN 1"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() error = %v, want deadline exceeded", err)
	}
}

func TestTimeoutExecutorIncludesStatementSummaryOnTimeout(t *testing.T) {
	t.Parallel()

	executor := TimeoutExecutor{
		Inner:   contextBlockingExecutor{},
		Timeout: 10 * time.Millisecond,
	}

	err := executor.Execute(context.Background(), Statement{
		Cypher: "RETURN 1",
		Parameters: map[string]any{
			StatementMetadataSummaryKey: "semantic label=Variable rows=500",
		},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() error = %v, want deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "semantic label=Variable rows=500") {
		t.Fatalf("Execute() error = %q, want statement summary", err)
	}
	var timeoutErr GraphWriteTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Execute() error = %T, want GraphWriteTimeoutError", err)
	}
	if got, want := timeoutErr.FailureClass(), "graph_write_timeout"; got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
	if got, want := timeoutErr.FailureDetails(), "semantic label=Variable rows=500"; got != want {
		t.Fatalf("FailureDetails() = %q, want %q", got, want)
	}
	var retryable interface {
		Retryable() bool
	}
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("Execute() error = %T, want retryable timeout", err)
	}
}

func TestTimeoutExecutorGraphWriteTimeoutSurvivesWrapping(t *testing.T) {
	t.Parallel()

	executor := TimeoutExecutor{
		Inner:   contextBlockingExecutor{},
		Timeout: 10 * time.Millisecond,
	}

	err := executor.Execute(context.Background(), Statement{
		Cypher: "RETURN 1",
		Parameters: map[string]any{
			StatementMetadataSummaryKey: "phase=files rows=100 chunk=21/24",
		},
	})
	wrapped := errors.New("missing timeout")
	if err != nil {
		wrapped = errors.Join(errors.New("canonical phase-group write"), err)
	}

	var timeoutErr GraphWriteTimeoutError
	if !errors.As(wrapped, &timeoutErr) {
		t.Fatalf("wrapped error = %T %v, want GraphWriteTimeoutError", wrapped, wrapped)
	}
	if !errors.Is(wrapped, context.DeadlineExceeded) {
		t.Fatalf("wrapped error = %v, want deadline exceeded", wrapped)
	}
	if got, want := timeoutErr.FailureDetails(), "phase=files rows=100 chunk=21/24"; got != want {
		t.Fatalf("FailureDetails() = %q, want %q", got, want)
	}
}

func TestTimeoutExecutorIncludesTimeoutHintOnTimeout(t *testing.T) {
	t.Parallel()

	executor := TimeoutExecutor{
		Inner:       contextBlockingExecutor{},
		Timeout:     10 * time.Millisecond,
		TimeoutHint: "ESHU_CANONICAL_WRITE_TIMEOUT",
	}

	err := executor.Execute(context.Background(), Statement{Cypher: "RETURN 1"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() error = %v, want deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "adjust ESHU_CANONICAL_WRITE_TIMEOUT") {
		t.Fatalf("Execute() error = %q, want timeout hint", err)
	}
}

func TestTimeoutExecutorZeroTimeoutPassesThrough(t *testing.T) {
	t.Parallel()

	inner := &recordingExecutor{}
	executor := TimeoutExecutor{
		Inner: inner,
	}

	err := executor.Execute(context.Background(), Statement{Cypher: "RETURN 1"})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if got, want := len(inner.calls), 1; got != want {
		t.Fatalf("inner calls = %d, want %d", got, want)
	}
}

func TestTimeoutExecutorExecuteGroupPassesThrough(t *testing.T) {
	t.Parallel()

	inner := &timeoutGroupRecordingExecutor{}
	executor := TimeoutExecutor{Inner: inner}

	err := executor.ExecuteGroup(context.Background(), []Statement{{Cypher: "RETURN 1"}})
	if err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil", err)
	}
	if inner.groupCalls != 1 {
		t.Fatalf("inner ExecuteGroup calls = %d, want 1", inner.groupCalls)
	}
}

func TestTimeoutExecutorExecuteGroupCancelsLongRunningGroup(t *testing.T) {
	t.Parallel()

	executor := TimeoutExecutor{
		Inner:   contextBlockingGroupExecutor{},
		Timeout: 10 * time.Millisecond,
	}

	err := executor.ExecuteGroup(context.Background(), []Statement{{Cypher: "RETURN 1"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ExecuteGroup() error = %v, want deadline exceeded", err)
	}
}

func TestTimeoutExecutorExecuteGroupErrorsWithoutGroupExecutor(t *testing.T) {
	t.Parallel()

	executor := TimeoutExecutor{Inner: &recordingExecutor{}}

	err := executor.ExecuteGroup(context.Background(), []Statement{{Cypher: "RETURN 1"}})
	if err == nil {
		t.Fatal("ExecuteGroup() error = nil, want non-nil")
	}
	if got, want := err.Error(), "inner executor does not support ExecuteGroup"; got != want {
		t.Fatalf("ExecuteGroup() error = %q, want %q", got, want)
	}
}

func TestTimeoutExecutorExecutePropagatesCancellationContext(t *testing.T) {
	t.Parallel()

	inner := &cancellationRecordingExecutor{}
	executor := TimeoutExecutor{
		Inner:   inner,
		Timeout: time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := executor.Execute(ctx, Statement{Cypher: "RETURN 1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context canceled", err)
	}
	if got, want := err.Error(), "neo4j execute canceled before completion: context canceled"; got != want {
		t.Fatalf("Execute() error = %q, want %q", got, want)
	}
}

func TestTimeoutExecutorExecuteGroupPropagatesCancellationContext(t *testing.T) {
	t.Parallel()

	inner := &cancellationRecordingGroupExecutor{}
	executor := TimeoutExecutor{
		Inner:   inner,
		Timeout: time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := executor.ExecuteGroup(ctx, []Statement{{Cypher: "RETURN 1"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteGroup() error = %v, want context canceled", err)
	}
	if got, want := err.Error(), "neo4j execute group canceled before completion: context canceled"; got != want {
		t.Fatalf("ExecuteGroup() error = %q, want %q", got, want)
	}
}

type contextBlockingExecutor struct{}

func (contextBlockingExecutor) Execute(ctx context.Context, _ Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

type contextBlockingGroupExecutor struct{}

func (contextBlockingGroupExecutor) Execute(ctx context.Context, _ Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

func (contextBlockingGroupExecutor) ExecuteGroup(ctx context.Context, _ []Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

type timeoutGroupRecordingExecutor struct {
	groupCalls int
}

func (e *timeoutGroupRecordingExecutor) Execute(context.Context, Statement) error {
	return nil
}

func (e *timeoutGroupRecordingExecutor) ExecuteGroup(context.Context, []Statement) error {
	e.groupCalls++
	return nil
}

type cancellationRecordingExecutor struct{}

func (cancellationRecordingExecutor) Execute(ctx context.Context, _ Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

type cancellationRecordingGroupExecutor struct{}

func (cancellationRecordingGroupExecutor) Execute(ctx context.Context, _ Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

func (cancellationRecordingGroupExecutor) ExecuteGroup(ctx context.Context, _ []Statement) error {
	<-ctx.Done()
	return ctx.Err()
}
