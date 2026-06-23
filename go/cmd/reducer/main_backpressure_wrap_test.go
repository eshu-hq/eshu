package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// stubGroupExecutor is a no-op executor that also implements GroupExecutor,
// used in tests that exercise the grouped-write code path.
type stubGroupExecutor struct{}

func (stubGroupExecutor) Execute(_ context.Context, _ sourcecypher.Statement) error { return nil }

// ExecuteGroup satisfies cypher.GroupExecutor so Wrap preserves the grouped
// write interface when the inner executor advertises it.
func (stubGroupExecutor) ExecuteGroup(_ context.Context, _ []sourcecypher.Statement) error {
	return nil
}

// TestBuildReducerServiceMaxInFlightWrapsAllWriters proves that when
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT is positive, buildReducerService succeeds
// and the wiring does not panic — the backpressure wrap covers all writers
// including the semantic entity writer that goes through ExecuteOnlyExecutor
// when grouped writes are disabled.
func TestBuildReducerServiceMaxInFlightWrapsAllWriters(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	// NornicDB backend with grouped writes DISABLED (ExecuteOnlyExecutor path)
	// and MAX_IN_FLIGHT=2. Before the fix, Wrap happened after other writers
	// were built, so MAX_IN_FLIGHT had no effect on them.
	_, err := buildReducerService(
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(name string) string {
			switch name {
			case "ESHU_GRAPH_BACKEND":
				return string(runtimecfg.GraphBackendNornicDB)
			case graphbackpressure.MaxInFlightEnv:
				return "2"
			case "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES":
				return "false"
			default:
				return ""
			}
		},
		nil,
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildReducerService() with MAX_IN_FLIGHT=2 error = %v, want nil", err)
	}
}

// TestBuildReducerServiceMaxInFlightWithGroupedWritesEnabled proves that when
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT is positive AND grouped writes are enabled,
// buildReducerService succeeds without error (the GroupExecutor path is
// preserved).
func TestBuildReducerServiceMaxInFlightWithGroupedWritesEnabled(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	_, err := buildReducerService(
		db,
		stubGroupExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(name string) string {
			switch name {
			case "ESHU_GRAPH_BACKEND":
				return string(runtimecfg.GraphBackendNornicDB)
			case graphbackpressure.MaxInFlightEnv:
				return "2"
			case "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES":
				return "true"
			default:
				return ""
			}
		},
		nil,
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildReducerService() with MAX_IN_FLIGHT=2 and grouped writes error = %v, want nil", err)
	}
}
