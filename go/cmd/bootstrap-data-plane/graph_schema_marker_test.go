package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

func TestRunSkipsGraphSchemaWhenFingerprintAlreadyApplied(t *testing.T) {
	t.Parallel()

	backend := graph.SchemaBackendNornicDB
	fingerprint, statementCount, err := graphSchemaFingerprint(backend)
	if err != nil {
		t.Fatalf("graphSchemaFingerprint() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{
			{rows: [][]any{{true}}},
		},
	}
	logger := testLogger(t)
	graphApplied := false

	err = run(
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
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if graphApplied {
		t.Fatal("run() applied graph schema, want same-fingerprint skip")
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got, want := db.queries[0].args[0], string(backend); got != want {
		t.Fatalf("marker backend arg = %q, want %q", got, want)
	}
	if got := db.queries[0].args[1]; got != fingerprint {
		t.Fatalf("marker fingerprint arg = %q, want %q", got, fingerprint)
	}
	if statementCount == 0 {
		t.Fatal("statement count = 0, want non-zero")
	}
}

func TestRunAppliesAndMarksGraphSchemaWhenFingerprintMissing(t *testing.T) {
	t.Parallel()

	backend := graph.SchemaBackendNornicDB
	fingerprint, statementCount, err := graphSchemaFingerprint(backend)
	if err != nil {
		t.Fatalf("graphSchemaFingerprint() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{{rows: nil}},
	}
	logger := testLogger(t)
	graphApplied := false

	err = run(
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
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !graphApplied {
		t.Fatal("run() did not apply graph schema")
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("marker exec count = %d, want %d", got, want)
	}
	if got, want := db.execs[0].args[0], string(backend); got != want {
		t.Fatalf("mark backend arg = %q, want %q", got, want)
	}
	if got := db.execs[0].args[1]; got != fingerprint {
		t.Fatalf("mark fingerprint arg = %q, want %q", got, fingerprint)
	}
	if got := db.execs[0].args[2]; got != statementCount {
		t.Fatalf("mark statement count arg = %v, want %d", got, statementCount)
	}
}

func TestRunDoesNotMarkGraphSchemaAfterApplyFailure(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{queryRows: []fakeBootstrapRows{{rows: nil}}}
	logger := testLogger(t)
	schemaErr := errors.New("graph schema failed")

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
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			return schemaErr
		},
	)
	if !errors.Is(err, schemaErr) {
		t.Fatalf("run() error = %v, want %v", err, schemaErr)
	}
	if len(db.execs) != 0 {
		t.Fatalf("marker exec count = %d, want 0", len(db.execs))
	}
}
