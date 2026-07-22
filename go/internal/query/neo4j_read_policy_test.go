// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNeo4jReaderUsesEarlierParentAndRemainingTransactionDeadline(t *testing.T) {
	var transactionTimeout time.Duration
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{run: func(
			_ context.Context,
			_ string,
			_ map[string]any,
			configurers ...func(*neo4jdriver.TransactionConfig),
		) (neo4jReadResult, error) {
			cfg := neo4jdriver.TransactionConfig{}
			for _, configure := range configurers {
				configure(&cfg)
			}
			transactionTimeout = cfg.Timeout
			return &fakeNeo4jReadResult{records: []*neo4jdriver.Record{{
				Keys: []string{"ok"}, Values: []any{true},
			}}}, nil
		}}
	})
	reader.policy.readTimeout = time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	rows, err := reader.Run(ctx, "RETURN true AS ok", nil)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if !BoolVal(rows[0], "ok") {
		t.Fatalf("Run() rows = %#v, want healthy result", rows)
	}
	if transactionTimeout <= 0 || transactionTimeout > 200*time.Millisecond {
		t.Fatalf("transaction timeout = %s, want remaining parent budget", transactionTimeout)
	}
}

func TestNeo4jReaderPolicyDeadlineIsPublicGraphDeadline(t *testing.T) {
	reader := newPolicyTestNeo4jReader(blockingPolicySession)
	reader.policy.readTimeout = 20 * time.Millisecond

	_, err := reader.Run(context.Background(), "RETURN 1", nil)
	if !errors.Is(err, ErrGraphReadDeadline) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want graph deadline wrapping context deadline", err)
	}
}

func TestNeo4jReaderPolicyDeadlineAllowsSessionCleanupToComplete(t *testing.T) {
	cleanupCompleted := false
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{
			result: &fakeNeo4jReadResult{collect: func(ctx context.Context) ([]*neo4jdriver.Record, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}},
			close: func(ctx context.Context) error {
				select {
				case <-time.After(10 * time.Millisecond):
					cleanupCompleted = true
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}
	})
	reader.policy.readTimeout = 20 * time.Millisecond
	started := time.Now()

	_, err := reader.Run(context.Background(), "RETURN 1", nil)
	if !errors.Is(err, ErrGraphReadDeadline) {
		t.Fatalf("Run() error = %v, want ErrGraphReadDeadline", err)
	}
	if !cleanupCompleted {
		t.Fatal("Run() returned before session cleanup completed")
	}
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Fatalf("Run() elapsed = %s, want completed cleanup within its separate bound", elapsed)
	}
}

func TestNeo4jReaderSessionCleanupUsesLiveBoundedContext(t *testing.T) {
	cleanupDeadline := time.Duration(0)
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{
			result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}},
			close: func(ctx context.Context) error {
				if err := ctx.Err(); err != nil {
					t.Fatalf("session cleanup context already expired: %v", err)
				}
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("session cleanup context has no deadline")
				}
				cleanupDeadline = time.Until(deadline)
				return nil
			},
		}
	})

	if _, err := reader.Run(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cleanupDeadline <= 0 || cleanupDeadline > graphReadSessionCloseTimeout {
		t.Fatalf("cleanup deadline = %s, want (0, %s]", cleanupDeadline, graphReadSessionCloseTimeout)
	}
}

func TestNeo4jReaderRepeatedTimeoutsCloseEverySession(t *testing.T) {
	const reads = 4
	sessions := 0
	closed := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		sessions++
		return &fakeNeo4jReadSession{
			result: &fakeNeo4jReadResult{collect: func(ctx context.Context) ([]*neo4jdriver.Record, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}},
			close: func(ctx context.Context) error {
				if err := ctx.Err(); err != nil {
					t.Fatalf("session %d cleanup context already expired: %v", sessions, err)
				}
				closed++
				return nil
			},
		}
	})
	reader.policy.readTimeout = 5 * time.Millisecond

	for range reads {
		if _, err := reader.Run(context.Background(), "RETURN 1", nil); !errors.Is(err, ErrGraphReadDeadline) {
			t.Fatalf("Run() error = %v, want ErrGraphReadDeadline", err)
		}
	}
	if sessions != reads || closed != reads {
		t.Fatalf("sessions/closed = %d/%d, want %d/%d", sessions, closed, reads, reads)
	}
}

func TestNeo4jReaderParentDeadlineWinsWithoutGraphDeadlineClassification(t *testing.T) {
	reader := newPolicyTestNeo4jReader(blockingPolicySession)
	reader.policy.readTimeout = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := reader.Run(ctx, "RETURN 1", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want parent context deadline", err)
	}
	if errors.Is(err, ErrGraphReadDeadline) {
		t.Fatalf("Run() error = %v, parent deadline must not become graph-policy deadline", err)
	}
}

func TestNeo4jReaderParentDeadlineWinsIfBackendReturnsRowsAfterCancellation(t *testing.T) {
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{collect: func(ctx context.Context) ([]*neo4jdriver.Record, error) {
			<-ctx.Done()
			return []*neo4jdriver.Record{{Keys: []string{"late"}, Values: []any{true}}}, nil
		}}}
	})
	reader.policy.readTimeout = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	rows, err := reader.Run(ctx, "RETURN true AS late", nil)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrGraphReadDeadline) {
		t.Fatalf("Run() error = %v, want unchanged parent deadline", err)
	}
	if rows != nil {
		t.Fatalf("Run() rows = %#v, want no rows after caller deadline", rows)
	}
}

func TestNeo4jReaderParentCancellationWinsWithoutOpeningRetry(t *testing.T) {
	sessions := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		sessions++
		return &fakeNeo4jReadSession{}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reader.Run(ctx, "RETURN 1", nil)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrGraphReadDeadline) {
		t.Fatalf("Run() error = %v, want unchanged parent cancellation", err)
	}
	if sessions != 0 {
		t.Fatalf("sessions = %d, want zero", sessions)
	}
}

func TestNeo4jReaderBackendTransactionTimeoutIsGraphDeadline(t *testing.T) {
	for _, code := range []string{neo4jTransactionTimedOutCode, neo4jTransactionTerminatedCode} {
		t.Run(code, func(t *testing.T) {
			reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
				return &fakeNeo4jReadSession{run: func(
					context.Context,
					string,
					map[string]any,
					...func(*neo4jdriver.TransactionConfig),
				) (neo4jReadResult, error) {
					return nil, &neo4jdriver.Neo4jError{Code: code, Msg: "private backend timeout detail"}
				}}
			})

			_, err := reader.Run(context.Background(), "RETURN 1", nil)
			if !errors.Is(err, ErrGraphReadDeadline) {
				t.Fatalf("Run() error = %v, want ErrGraphReadDeadline", err)
			}
		})
	}
}

func TestNeo4jReaderRetriesTypedConnectivityOnceInsideSameBudget(t *testing.T) {
	attempts := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		attempts++
		if attempts == 1 {
			return &fakeNeo4jReadSession{run: connectivityErrorRun("temporary disconnect")}
		}
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{{
			Keys: []string{"ok"}, Values: []any{true},
		}}}}
	})

	rows, err := reader.Run(context.Background(), "RETURN true AS ok", nil)
	if err != nil {
		t.Fatalf("Run() error = %v, want recovered success", err)
	}
	if attempts != 2 || !BoolVal(rows[0], "ok") {
		t.Fatalf("attempts=%d rows=%#v, want two attempts and recovered row", attempts, rows)
	}
}

func TestNeo4jReaderRetriesRetryableNeo4jAvailabilityErrorOnce(t *testing.T) {
	attempts := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		attempts++
		if attempts == 1 {
			return &fakeNeo4jReadSession{run: func(
				context.Context,
				string,
				map[string]any,
				...func(*neo4jdriver.TransactionConfig),
			) (neo4jReadResult, error) {
				return nil, &neo4jdriver.Neo4jError{
					Code: "Neo.TransientError.General.DatabaseUnavailable",
					Msg:  "private availability detail",
				}
			}}
		}
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{{
			Keys: []string{"ok"}, Values: []any{true},
		}}}}
	})

	rows, err := reader.Run(context.Background(), "RETURN true AS ok", nil)
	if err != nil || attempts != maxGraphReadAttempts || !BoolVal(rows[0], "ok") {
		t.Fatalf("Run() = (%#v, %v, %d attempts), want recovered availability retry", rows, err, attempts)
	}
}

func TestNeo4jReaderTerminalRetryableNeo4jErrorIsUnavailableAndSanitized(t *testing.T) {
	const privateCause = "bolt://private-availability.example.invalid:7687"
	attempts := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		attempts++
		return &fakeNeo4jReadSession{run: func(
			context.Context,
			string,
			map[string]any,
			...func(*neo4jdriver.TransactionConfig),
		) (neo4jReadResult, error) {
			return nil, &neo4jdriver.Neo4jError{
				Code: "Neo.TransientError.General.DatabaseUnavailable",
				Msg:  privateCause,
			}
		}}
	})

	_, err := reader.Run(context.Background(), "RETURN 1", nil)
	if !errors.Is(err, ErrGraphUnavailable) || attempts != maxGraphReadAttempts {
		t.Fatalf("Run() = (%v, %d attempts), want unavailable after bounded retry", err, attempts)
	}
	if strings.Contains(err.Error(), privateCause) {
		t.Fatalf("Run() exposed Neo4j error detail: %v", err)
	}
}

func TestNeo4jReaderUnavailableAtStartIsRetriedAndSanitized(t *testing.T) {
	const privateCause = "bolt://private.example.invalid:7687"
	attempts := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		attempts++
		return &fakeNeo4jReadSession{run: connectivityErrorRun(privateCause)}
	})

	_, err := reader.Run(context.Background(), "RETURN 1", nil)
	if !errors.Is(err, ErrGraphUnavailable) || attempts != maxGraphReadAttempts {
		t.Fatalf("Run() = (%v, %d attempts), want unavailable after %d attempts", err, attempts, maxGraphReadAttempts)
	}
	if strings.Contains(err.Error(), privateCause) {
		t.Fatalf("Run() leaked private cause: %q", err)
	}
}

func TestNeo4jReaderRetriesConnectivityFailureDuringCollection(t *testing.T) {
	attempts := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		attempts++
		if attempts == 1 {
			return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{
				collectErr: &neo4jdriver.ConnectivityError{Inner: errors.New("connection reset")},
			}}
		}
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{{
			Keys: []string{"ok"}, Values: []any{true},
		}}}}
	})

	rows, err := reader.Run(context.Background(), "RETURN true AS ok", nil)
	if err != nil || attempts != maxGraphReadAttempts || !BoolVal(rows[0], "ok") {
		t.Fatalf("Run() = (%#v, %v, %d attempts), want recovered row", rows, err, attempts)
	}
}

func TestNeo4jReaderDoesNotRetryUntypedErrors(t *testing.T) {
	attempts := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		attempts++
		return &fakeNeo4jReadSession{run: func(
			context.Context,
			string,
			map[string]any,
			...func(*neo4jdriver.TransactionConfig),
		) (neo4jReadResult, error) {
			return nil, errors.New("syntax error")
		}}
	})

	if _, err := reader.Run(context.Background(), "BROKEN", nil); err == nil {
		t.Fatal("Run() error = nil, want query error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func blockingPolicySession(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
	return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{collect: func(ctx context.Context) ([]*neo4jdriver.Record, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}}
}

func newPolicyTestNeo4jReader(factory neo4jReadSessionFactory) *Neo4jReader {
	return &Neo4jReader{
		database:       "neo4j",
		tracer:         noop.NewTracerProvider().Tracer("test"),
		policy:         defaultNeo4jReadPolicy(),
		sessionFactory: factory,
	}
}

type fakeNeo4jReadSession struct {
	run    func(context.Context, string, map[string]any, ...func(*neo4jdriver.TransactionConfig)) (neo4jReadResult, error)
	result neo4jReadResult
	close  func(context.Context) error
}

func (s *fakeNeo4jReadSession) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
	configurers ...func(*neo4jdriver.TransactionConfig),
) (neo4jReadResult, error) {
	if s.run != nil {
		return s.run(ctx, cypher, params, configurers...)
	}
	return s.result, nil
}

func (s *fakeNeo4jReadSession) Close(ctx context.Context) error {
	if s.close != nil {
		return s.close(ctx)
	}
	return nil
}

type fakeNeo4jReadResult struct {
	records    []*neo4jdriver.Record
	collectErr error
	collect    func(context.Context) ([]*neo4jdriver.Record, error)
}

func (r *fakeNeo4jReadResult) Collect(ctx context.Context) ([]*neo4jdriver.Record, error) {
	if r.collect != nil {
		return r.collect(ctx)
	}
	return r.records, r.collectErr
}

func connectivityErrorRun(message string) func(
	context.Context,
	string,
	map[string]any,
	...func(*neo4jdriver.TransactionConfig),
) (neo4jReadResult, error) {
	return func(context.Context, string, map[string]any, ...func(*neo4jdriver.TransactionConfig)) (neo4jReadResult, error) {
		return nil, &neo4jdriver.ConnectivityError{Inner: errors.New(message)}
	}
}
