package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

func TestRunSkipsGraphSchemaWhenLatestFingerprintAlreadyApplied(t *testing.T) {
	t.Parallel()

	backend := graph.SchemaBackendNornicDB
	fingerprint, statementCount, err := graphSchemaFingerprint(backend)
	if err != nil {
		t.Fatalf("graphSchemaFingerprint() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{
			{rows: [][]any{{fingerprint, []byte(`[]`)}}},
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
	if statementCount == 0 {
		t.Fatal("statement count = 0, want non-zero")
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("marker refresh exec count = %d, want %d", got, want)
	}
	if got := db.execs[0].args[1]; got != fingerprint {
		t.Fatalf("mark fingerprint arg = %q, want %q", got, fingerprint)
	}
	if got := db.execs[0].args[2]; got != statementCount {
		t.Fatalf("mark statement count arg = %v, want %d", got, statementCount)
	}
}

func TestRunSkipsGraphSchemaWhenLatestMarkerListsFingerprintCompatible(t *testing.T) {
	t.Parallel()

	backend := graph.SchemaBackendNornicDB
	fingerprint, _, err := graphSchemaFingerprint(backend)
	if err != nil {
		t.Fatalf("graphSchemaFingerprint() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{
			{rows: [][]any{{"future-additive-fingerprint", []byte(`["` + fingerprint + `"]`)}}},
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
		t.Fatal("run() applied graph schema, want compatible latest-marker skip")
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("marker refresh exec count = %d, want 0", got)
	}
}

func TestRunAppliesAndMarksGraphSchemaWhenFingerprintMissing(t *testing.T) {
	t.Parallel()

	backend := graph.SchemaBackendNornicDB
	fingerprint, statementCount, err := graphSchemaFingerprint(backend)
	if err != nil {
		t.Fatalf("graphSchemaFingerprint() error = %v, want nil", err)
	}
	app, err := graph.SchemaApplicationForBackend(backend)
	if err != nil {
		t.Fatalf("SchemaApplicationForBackend() error = %v, want nil", err)
	}
	compatibleFingerprints, err := json.Marshal(app.CompatibleFingerprints)
	if err != nil {
		t.Fatalf("marshal compatible fingerprints: %v", err)
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
	if got, want := db.execs[0].args[3], string(compatibleFingerprints); got != want {
		t.Fatalf("mark compatible fingerprints arg = %v, want %q", got, want)
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

func TestRunAdoptsExistingGraphSchemaWhenMarkerMissing(t *testing.T) {
	t.Parallel()

	backend := graph.SchemaBackendNornicDB
	fingerprint, statementCount, err := graphSchemaFingerprint(backend)
	if err != nil {
		t.Fatalf("graphSchemaFingerprint() error = %v, want nil", err)
	}
	expectedNames, err := expectedGraphSchemaObjectNames(backend)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{{rows: nil}},
	}
	inspector := &fakeGraphSchemaInspector{names: expectedNames}
	logger := testLogger(t)
	graphApplied := false

	err = run(
		context.Background(),
		func(key string) string {
			if key == graphSchemaAdoptExistingEnv {
				return "true"
			}
			return ""
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor:  &fakeNeo4jExecutor{},
				inspector: inspector,
				close:     func() error { return nil },
			}, nil
		},
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if graphApplied {
		t.Fatal("run() applied graph schema, want existing schema adoption")
	}
	if got, want := inspector.calls, 1; got != want {
		t.Fatalf("schema inspector calls = %d, want %d", got, want)
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

func TestRunAppliesGraphSchemaWhenAdoptionFindsMissingObjects(t *testing.T) {
	t.Parallel()

	expectedNames, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v, want nil", err)
	}
	existingNames := maps.Clone(expectedNames)
	delete(existingNames, "repository_id")
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{{rows: nil}},
	}
	inspector := &fakeGraphSchemaInspector{names: existingNames}
	logger := testLogger(t)
	graphApplied := false

	err = run(
		context.Background(),
		func(key string) string {
			if key == graphSchemaAdoptExistingEnv {
				return "true"
			}
			return ""
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor:  &fakeNeo4jExecutor{},
				inspector: inspector,
				close:     func() error { return nil },
			}, nil
		},
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !graphApplied {
		t.Fatal("run() did not apply graph schema, want fallback when adoption is incomplete")
	}
	if got, want := inspector.calls, 1; got != want {
		t.Fatalf("schema inspector calls = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("marker exec count = %d, want %d", got, want)
	}
}

func TestRunBoundsGraphSchemaAdoptionInspection(t *testing.T) {
	t.Parallel()

	expectedNames, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{{rows: nil}},
	}
	inspector := &fakeGraphSchemaInspector{names: expectedNames}
	logger := testLogger(t)

	err = run(
		context.Background(),
		func(key string) string {
			switch key {
			case graphSchemaAdoptExistingEnv:
				return "true"
			case graphSchemaStatementTimeoutEnv:
				return "3s"
			default:
				return ""
			}
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor:  &fakeNeo4jExecutor{},
				inspector: inspector,
				close:     func() error { return nil },
			}, nil
		},
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			t.Fatal("run() applied graph schema, want existing schema adoption")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !inspector.sawDeadline {
		t.Fatal("schema inspector saw no deadline")
	}
	if inspector.deadlineRemaining <= 0 || inspector.deadlineRemaining > 3*time.Second {
		t.Fatalf("schema inspector deadline remaining = %s, want within 3s", inspector.deadlineRemaining)
	}
}

func TestRunFailsWhenAdoptionInspectionFails(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{{rows: nil}},
	}
	inspectErr := errors.New("show schema failed")
	inspector := &fakeGraphSchemaInspector{err: inspectErr}
	logger := testLogger(t)
	graphApplied := false

	err := run(
		context.Background(),
		func(key string) string {
			if key == graphSchemaAdoptExistingEnv {
				return "true"
			}
			return ""
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor:  &fakeNeo4jExecutor{},
				inspector: inspector,
				close:     func() error { return nil },
			}, nil
		},
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if !errors.Is(err, inspectErr) {
		t.Fatalf("run() error = %v, want %v", err, inspectErr)
	}
	if graphApplied {
		t.Fatal("run() applied graph schema after inspection failure")
	}
	if len(db.execs) != 0 {
		t.Fatalf("marker exec count = %d, want 0", len(db.execs))
	}
}
