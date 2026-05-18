package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopNeo4j(_ context.Context, _ func(string) string) (neo4jDeps, error) {
	return neo4jDeps{
		executor: &fakeNeo4jExecutor{},
		close:    func() error { return nil },
	}, nil
}

func noopApplyNeo4j(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
	return nil
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()

	bootstrap, err := telemetry.NewBootstrap("eshu-bootstrap-data-plane")
	require.NoError(t, err)

	return newLogger(bootstrap, io.Discard)
}

func TestNewLoggerOutputsJSON(t *testing.T) {
	var buf bytes.Buffer

	bootstrap, err := telemetry.NewBootstrap("eshu-bootstrap-data-plane")
	require.NoError(t, err)

	logger := newLogger(bootstrap, &buf)
	logger.Info("bootstrap schema migration started", slog.String("phase", "bootstrap"))

	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, "eshu-bootstrap-data-plane", logEntry["service_name"])
	assert.Equal(t, "bootstrap", logEntry["component"])
	assert.Equal(t, "bootstrap-data-plane", logEntry["runtime_role"])
	assert.Equal(t, "bootstrap schema migration started", logEntry["message"])
	assert.Equal(t, "INFO", logEntry["severity_text"])
}

func TestRunAppliesPostgresAndDefaultGraphSchema(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{}
	pgApplied := false
	graphApplied := false
	logger := testLogger(t)

	err := run(
		context.Background(),
		func(string) string { return "" },
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(ctx context.Context, exec bootstrapExecutor) error {
			pgApplied = true
			if exec != db {
				t.Fatalf("apply exec = %T, want fakeBootstrapDB", exec)
			}
			_, _ = exec.ExecContext(ctx, "SELECT 1")
			return nil
		},
		noopNeo4j,
		func(_ context.Context, exec graph.CypherExecutor, _ *slog.Logger, backend graph.SchemaBackend) error {
			graphApplied = true
			if exec == nil {
				t.Fatal("neo4j executor is nil")
			}
			if backend != graph.SchemaBackendNornicDB {
				t.Fatalf("schema backend = %q, want %q", backend, graph.SchemaBackendNornicDB)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !pgApplied {
		t.Fatal("run() did not apply postgres schema")
	}
	if !graphApplied {
		t.Fatal("run() did not apply graph schema")
	}
	if !db.closed {
		t.Fatal("run() did not close postgres database")
	}
}

func TestRunPassesNeo4jBackendToSchemaApplicator(t *testing.T) {
	t.Parallel()

	logger := testLogger(t)
	var gotBackend graph.SchemaBackend

	err := run(
		context.Background(),
		func(key string) string {
			if key == "ESHU_GRAPH_BACKEND" {
				return "neo4j"
			}
			return ""
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return &fakeBootstrapDB{}, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		noopNeo4j,
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, backend graph.SchemaBackend) error {
			gotBackend = backend
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if gotBackend != graph.SchemaBackendNeo4j {
		t.Fatalf("schema backend = %q, want %q", gotBackend, graph.SchemaBackendNeo4j)
	}
}

func TestRunPassesNornicDBBackendToSchemaApplicator(t *testing.T) {
	t.Parallel()

	logger := testLogger(t)
	var gotBackend graph.SchemaBackend

	err := run(
		context.Background(),
		func(key string) string {
			if key == "ESHU_GRAPH_BACKEND" {
				return "nornicdb"
			}
			return ""
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return &fakeBootstrapDB{}, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		noopNeo4j,
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, backend graph.SchemaBackend) error {
			gotBackend = backend
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if gotBackend != graph.SchemaBackendNornicDB {
		t.Fatalf("schema backend = %q, want %q", gotBackend, graph.SchemaBackendNornicDB)
	}
}

func TestRunReturnsCloseErrorWhenBootstrapSucceeds(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{closeErr: errors.New("close failed")}
	logger := testLogger(t)

	err := run(
		context.Background(),
		func(string) string { return "" },
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		noopNeo4j,
		noopApplyNeo4j,
	)
	if err == nil {
		t.Fatal("run() error = nil, want non-nil")
	}
	if got := err.Error(); got != "close failed" {
		t.Fatalf("run() error = %q, want %q", got, "close failed")
	}
	if !db.closed {
		t.Fatal("run() did not close bootstrap database")
	}
}

func TestRunJoinsBootstrapAndCloseErrors(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{closeErr: errors.New("close failed")}
	bootstrapErr := errors.New("bootstrap failed")
	logger := testLogger(t)

	err := run(
		context.Background(),
		func(string) string { return "" },
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return bootstrapErr
		},
		noopNeo4j,
		noopApplyNeo4j,
	)
	if err == nil {
		t.Fatal("run() error = nil, want non-nil")
	}
	if !errors.Is(err, bootstrapErr) {
		t.Fatalf("run() error does not include bootstrap error: %v", err)
	}
	if !errors.Is(err, db.closeErr) {
		t.Fatalf("run() error does not include close error: %v", err)
	}
	if !db.closed {
		t.Fatal("run() did not close bootstrap database")
	}
}

func TestRunReturnsNeo4jOpenError(t *testing.T) {
	t.Parallel()

	neo4jErr := errors.New("neo4j connection refused")
	logger := testLogger(t)

	err := run(
		context.Background(),
		func(string) string { return "" },
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return &fakeBootstrapDB{}, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{}, neo4jErr
		},
		noopApplyNeo4j,
	)
	if !errors.Is(err, neo4jErr) {
		t.Fatalf("run() error = %v, want %v", err, neo4jErr)
	}
}

func TestRunReturnsNeo4jSchemaError(t *testing.T) {
	t.Parallel()

	schemaErr := errors.New("neo4j schema failed")
	logger := testLogger(t)

	err := run(
		context.Background(),
		func(string) string { return "" },
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return &fakeBootstrapDB{}, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		noopNeo4j,
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			return schemaErr
		},
	)
	if !errors.Is(err, schemaErr) {
		t.Fatalf("run() error = %v, want %v", err, schemaErr)
	}
}

func TestRunWrapsGraphSchemaStatementsWithConfiguredTimeout(t *testing.T) {
	t.Parallel()

	executor := &deadlineRecordingExecutor{}
	logger := testLogger(t)

	err := run(
		context.Background(),
		func(key string) string {
			if key == graphSchemaStatementTimeoutEnv {
				return "25ms"
			}
			return ""
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return &fakeBootstrapDB{}, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor: executor,
				close:    func() error { return nil },
			}, nil
		},
		func(ctx context.Context, exec graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			return exec.ExecuteCypher(ctx, graph.CypherStatement{Cypher: "CREATE INDEX test IF NOT EXISTS FOR (n:Node) ON (n.id)"})
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !executor.sawDeadline {
		t.Fatal("graph schema executor did not receive a statement deadline")
	}
	if executor.deadlineRemaining <= 0 || executor.deadlineRemaining > 100*time.Millisecond {
		t.Fatalf("deadline remaining = %s, want a bounded per-statement timeout", executor.deadlineRemaining)
	}
}

func TestRunRejectsInvalidGraphSchemaStatementTimeout(t *testing.T) {
	t.Parallel()

	logger := testLogger(t)

	err := run(
		context.Background(),
		func(key string) string {
			if key == graphSchemaStatementTimeoutEnv {
				return "soon"
			}
			return ""
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return &fakeBootstrapDB{}, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		noopNeo4j,
		noopApplyNeo4j,
	)
	if err == nil {
		t.Fatal("run() error = nil, want invalid timeout error")
	}
	if !strings.Contains(err.Error(), graphSchemaStatementTimeoutEnv) {
		t.Fatalf("run() error = %q, want env var name", err.Error())
	}
}

func TestRunJoinsNeo4jCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("neo4j close failed")
	logger := testLogger(t)

	err := run(
		context.Background(),
		func(string) string { return "" },
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return &fakeBootstrapDB{}, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor: &fakeNeo4jExecutor{},
				close:    func() error { return closeErr },
			}, nil
		},
		noopApplyNeo4j,
	)
	if !errors.Is(err, closeErr) {
		t.Fatalf("run() error = %v, want neo4j close error", err)
	}
}

type fakeBootstrapDB struct {
	execCalls int
	closed    bool
	closeErr  error
}

func (f *fakeBootstrapDB) ExecContext(
	context.Context,
	string,
	...any,
) (sql.Result, error) {
	f.execCalls++
	return fakeBootstrapResult{}, nil
}

func (f *fakeBootstrapDB) Close() error {
	f.closed = true
	return f.closeErr
}

type fakeBootstrapResult struct{}

func (fakeBootstrapResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeBootstrapResult) RowsAffected() (int64, error) { return 0, nil }

type fakeNeo4jExecutor struct {
	calls int
}

func (f *fakeNeo4jExecutor) ExecuteCypher(_ context.Context, _ graph.CypherStatement) error {
	f.calls++
	return nil
}

type deadlineRecordingExecutor struct {
	sawDeadline       bool
	deadlineRemaining time.Duration
}

func (d *deadlineRecordingExecutor) ExecuteCypher(ctx context.Context, _ graph.CypherStatement) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil
	}
	d.sawDeadline = true
	d.deadlineRemaining = time.Until(deadline)
	return nil
}
