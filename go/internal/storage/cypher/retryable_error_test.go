package cypher

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newNeo4jError constructs a real driver Neo4jError for testing.
func newNeo4jError(code, msg string) *neo4jdriver.Neo4jError {
	return &neo4jdriver.Neo4jError{Code: code, Msg: msg}
}

func TestWrapRetryableNeo4jError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		wantRetryable  bool
		wantWrapped    bool // true when WrapRetryableNeo4jError should return a different error
		wantMessage    string
		skipNeo4jCheck bool // TransactionExecutionLimit doesn't implement Unwrap
	}{
		{
			name:          "nil error returns nil",
			err:           nil,
			wantRetryable: false,
			wantWrapped:   false,
		},
		{
			name:          "EntityNotFound is retryable",
			err:           newNeo4jError("Neo.ClientError.Statement.EntityNotFound", "Unable to load NODE 4:abc:123"),
			wantRetryable: true,
			wantWrapped:   true,
			wantMessage:   "Unable to load NODE 4:abc:123",
		},
		{
			name:          "DeadlockDetected is retryable",
			err:           newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock detected"),
			wantRetryable: true,
			wantWrapped:   true,
			wantMessage:   "deadlock detected",
		},
		{
			name:          "other Neo4j code is not retryable",
			err:           newNeo4jError("Neo.ClientError.Schema.ConstraintValidationFailed", "constraint failed"),
			wantRetryable: false,
			wantWrapped:   false,
			wantMessage:   "constraint failed",
		},
		{
			name:          "plain error without Neo4j type is not retryable",
			err:           errors.New("connection reset"),
			wantRetryable: false,
			wantWrapped:   false,
			wantMessage:   "connection reset",
		},
		{
			name: "wrapped EntityNotFound preserves retryable through chain",
			err: fmt.Errorf("write semantic entities: %w",
				newNeo4jError("Neo.ClientError.Statement.EntityNotFound", "node gone")),
			wantRetryable: true,
			wantWrapped:   true,
			wantMessage:   "node gone",
		},
		{
			name: "wrapped DeadlockDetected preserves retryable through chain",
			err: fmt.Errorf("retract edges: %w",
				newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock")),
			wantRetryable: true,
			wantWrapped:   true,
			wantMessage:   "deadlock",
		},
		{
			name: "TransactionExecutionLimit is retryable",
			err: &neo4jdriver.TransactionExecutionLimit{
				Cause: "timeout (exceeded max retry time: 30s)",
				Errors: []error{
					newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock"),
				},
			},
			wantRetryable:  true,
			wantWrapped:    true,
			wantMessage:    "TransactionExecutionLimit",
			skipNeo4jCheck: true,
		},
		{
			name: "wrapped TransactionExecutionLimit is retryable",
			err: fmt.Errorf("write canonical code calls: %w", &neo4jdriver.TransactionExecutionLimit{
				Cause: "timeout (exceeded max retry time: 30s)",
				Errors: []error{
					newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock"),
				},
			}),
			wantRetryable:  true,
			wantWrapped:    true,
			wantMessage:    "TransactionExecutionLimit",
			skipNeo4jCheck: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := WrapRetryableNeo4jError(tt.err)

			if tt.err == nil {
				assert.Nil(t, result)
				return
			}

			if !tt.wantWrapped {
				// Error should be returned unchanged
				assert.Same(t, tt.err, result, "non-retryable error should be returned as-is")
				assert.False(t, reducer.IsRetryable(result), "non-retryable error should not satisfy IsRetryable")
				return
			}

			// Error should be wrapped as retryable
			require.NotNil(t, result)
			assert.True(t, reducer.IsRetryable(result), "wrapped error should satisfy reducer.IsRetryable()")
			assert.Contains(t, result.Error(), tt.wantMessage, "error message should be preserved")

			// Original Neo4j error should be accessible via Unwrap
			// (TransactionExecutionLimit doesn't implement Unwrap, so the inner Neo4jError is unreachable)
			if !tt.skipNeo4jCheck {
				var neo4jErr *neo4jdriver.Neo4jError
				assert.True(t, errors.As(result, &neo4jErr), "original Neo4j error should be reachable via errors.As")
			}
		})
	}
}

// TestProductionErrorChain replicates the exact error path from production:
// session.ExecuteWrite exhausts retries → *TransactionExecutionLimit
// → EdgeWriter.WrapRetryableNeo4jError → handler fmt.Errorf wraps
// → queue checks reducer.IsRetryable
func TestProductionErrorChain(t *testing.T) {
	t.Parallel()

	// Step 1: Driver returns *TransactionExecutionLimit after exhausting 30s retry
	driverErr := &neo4jdriver.TransactionExecutionLimit{
		Cause: "timeout (exceeded max retry time: 30s)",
		Errors: []error{
			newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock cycle"),
			newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock cycle"),
			newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock cycle"),
			newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock cycle"),
		},
	}

	// Step 2: EdgeWriter calls WrapRetryableNeo4jError
	edgeWriterErr := WrapRetryableNeo4jError(driverErr)

	// Step 3: Handler wraps with context
	handlerErr := fmt.Errorf("write canonical code calls: %w", edgeWriterErr)

	// Step 4: Queue checks IsRetryable — THIS is the critical assertion
	assert.True(t, reducer.IsRetryable(handlerErr),
		"queue must see TransactionExecutionLimit as retryable through the full error chain")

	// Verify the intermediate steps
	assert.True(t, reducer.IsRetryable(edgeWriterErr),
		"EdgeWriter output must be retryable")
	assert.NotSame(t, driverErr, edgeWriterErr,
		"WrapRetryableNeo4jError should have wrapped the error")
}

func TestNeo4jRetryableErrorImplementsInterface(t *testing.T) {
	t.Parallel()

	inner := newNeo4jError("Neo.ClientError.Statement.EntityNotFound", "node gone")
	wrapped := WrapRetryableNeo4jError(inner)

	// Verify it implements reducer.RetryableError
	var retryable reducer.RetryableError
	require.True(t, errors.As(wrapped, &retryable))
	assert.True(t, retryable.Retryable())

	// Verify Unwrap chain preserves the original
	assert.True(t, errors.Is(wrapped, inner))
}
