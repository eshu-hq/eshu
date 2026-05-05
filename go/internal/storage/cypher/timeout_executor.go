package cypher

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TimeoutExecutor bounds individual graph write statements with a child
// context. A zero timeout preserves the caller's context unchanged.
type TimeoutExecutor struct {
	Inner       Executor
	Timeout     time.Duration
	TimeoutHint string
}

// GraphWriteTimeoutError marks a graph write deadline/cancellation with enough
// context for queue failure classifiers and operator status surfaces.
type GraphWriteTimeoutError struct {
	Operation   string
	Timeout     time.Duration
	TimeoutHint string
	Summary     string
	Cause       error
}

func (e GraphWriteTimeoutError) Error() string {
	prefix := e.Operation
	if e.Timeout > 0 {
		prefix = fmt.Sprintf("%s after %s", prefix, e.Timeout)
	}
	if e.TimeoutHint != "" {
		prefix = fmt.Sprintf("%s; adjust %s to tune the graph write budget", prefix, e.TimeoutHint)
	}
	if e.Summary != "" {
		return fmt.Sprintf("%s (%s): %v", prefix, e.Summary, e.Cause)
	}
	return fmt.Sprintf("%s: %v", prefix, e.Cause)
}

func (e GraphWriteTimeoutError) Unwrap() error {
	return e.Cause
}

func (e GraphWriteTimeoutError) FailureClass() string {
	return "graph_write_timeout"
}

func (e GraphWriteTimeoutError) FailureDetails() string {
	if e.Summary == "" {
		return e.Error()
	}
	return e.Summary
}

// Retryable marks graph write deadlines as bounded-retry candidates. A timeout
// can be caused by transient backend pressure, while syntax/schema failures
// still remain terminal because they do not implement the retry contract.
func (e GraphWriteTimeoutError) Retryable() bool {
	return true
}

// Execute forwards the statement with an optional deadline.
func (e TimeoutExecutor) Execute(ctx context.Context, statement Statement) error {
	if e.Inner == nil {
		return fmt.Errorf("inner executor is required")
	}
	if e.Timeout <= 0 {
		return e.Inner.Execute(ctx, statement)
	}

	execCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()

	err := e.Inner.Execute(execCtx, statement)
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return timeoutError(
			"neo4j execute timed out",
			e.Timeout,
			e.TimeoutHint,
			statementSummary(statement),
			context.DeadlineExceeded,
		)
	}
	if errors.Is(execCtx.Err(), context.Canceled) {
		return timeoutError(
			"neo4j execute canceled before completion",
			0,
			"",
			statementSummary(statement),
			context.Canceled,
		)
	}
	return err
}

// ExecuteGroup forwards grouped statements with an optional deadline when the
// wrapped executor supports atomic grouped writes.
func (e TimeoutExecutor) ExecuteGroup(ctx context.Context, statements []Statement) error {
	if e.Inner == nil {
		return fmt.Errorf("inner executor is required")
	}
	ge, ok := e.Inner.(GroupExecutor)
	if !ok {
		return fmt.Errorf("inner executor does not support ExecuteGroup")
	}
	if e.Timeout <= 0 {
		return ge.ExecuteGroup(ctx, statements)
	}

	execCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()

	err := ge.ExecuteGroup(execCtx, statements)
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return timeoutError(
			"neo4j execute group timed out",
			e.Timeout,
			e.TimeoutHint,
			statementGroupSummary(statements),
			context.DeadlineExceeded,
		)
	}
	if errors.Is(execCtx.Err(), context.Canceled) {
		return timeoutError(
			"neo4j execute group canceled before completion",
			0,
			"",
			statementGroupSummary(statements),
			context.Canceled,
		)
	}
	return err
}

func timeoutError(prefix string, timeout time.Duration, timeoutHint string, summary string, cause error) error {
	if errors.Is(cause, context.DeadlineExceeded) {
		return GraphWriteTimeoutError{
			Operation:   prefix,
			Timeout:     timeout,
			TimeoutHint: timeoutHint,
			Summary:     summary,
			Cause:       cause,
		}
	}
	if timeout > 0 {
		prefix = fmt.Sprintf("%s after %s", prefix, timeout)
	}
	if timeoutHint != "" {
		prefix = fmt.Sprintf("%s; adjust %s to tune the graph write budget", prefix, timeoutHint)
	}
	if summary != "" {
		return fmt.Errorf("%s (%s): %w", prefix, summary, cause)
	}
	return fmt.Errorf("%s: %w", prefix, cause)
}

func statementGroupSummary(statements []Statement) string {
	if len(statements) == 0 {
		return ""
	}
	return statementSummary(statements[0])
}

func statementSummary(statement Statement) string {
	if statement.Parameters == nil {
		return ""
	}
	summary, _ := statement.Parameters[StatementMetadataSummaryKey].(string)
	return summary
}
