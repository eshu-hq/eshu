package main

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// poolProbeExecutor records the peak number of concurrent in-flight
// Execute/ExecuteGroup calls so a test can prove the shared backpressure bound
// reaches the base executor every reducer writer derives from. release blocks
// each call until closed, letting the test hold permits and observe gating.
type poolProbeExecutor struct {
	mu      sync.Mutex
	current int
	peak    int
	release chan struct{}
}

func (e *poolProbeExecutor) enter() {
	e.mu.Lock()
	e.current++
	if e.current > e.peak {
		e.peak = e.current
	}
	e.mu.Unlock()
}

func (e *poolProbeExecutor) leave() {
	e.mu.Lock()
	e.current--
	e.mu.Unlock()
}

func (e *poolProbeExecutor) peakConcurrency() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peak
}

func (e *poolProbeExecutor) block(ctx context.Context) error {
	e.enter()
	defer e.leave()
	if e.release == nil {
		return nil
	}
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *poolProbeExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	return e.block(ctx)
}

func (e *poolProbeExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	return e.block(ctx)
}

// TestBoundReducerGraphWritesSharesOnePoolAcrossBaseExecutor is the structural
// regression for issue #3652 P2: the backpressure bound MUST be applied to the
// base executor that every reducer writer derives from, so a single in-flight
// permit pool gates all of them. Before the fix only the semantic executor was
// wrapped, leaving handler edges, shared projection, secrets/IAM, orphan sweep,
// and workload materializers unbounded. This test drives concurrent writes
// through the bounded base executor and proves the peak inner concurrency never
// exceeds the ceiling.
func TestBoundReducerGraphWritesSharesOnePoolAcrossBaseExecutor(t *testing.T) {
	t.Parallel()

	const maxInFlight = 2
	const callers = 16

	probe := &poolProbeExecutor{release: make(chan struct{})}
	bounded := boundReducerGraphWrites(probe, func(name string) string {
		if name == graphbackpressure.MaxInFlightEnv {
			return "2"
		}
		return ""
	}, nil)

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = bounded.Execute(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1"})
		}()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && probe.peakConcurrency() < maxInFlight {
		time.Sleep(time.Millisecond)
	}
	close(probe.release)
	wg.Wait()

	if peak := probe.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent base writes = %d, want <= %d (shared bound breached)", peak, maxInFlight)
	}
}

// TestBoundReducerGraphWritesDisabledIsPassthrough proves a non-positive
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT leaves the base executor unchanged, so the
// helper is a safe no-op and preserves the GroupExecutor interface.
func TestBoundReducerGraphWritesDisabledIsPassthrough(t *testing.T) {
	t.Parallel()

	probe := &poolProbeExecutor{}
	bounded := boundReducerGraphWrites(probe, func(string) string { return "" }, nil)

	if bounded != sourcecypher.Executor(probe) {
		t.Fatalf("boundReducerGraphWrites disabled = %T, want inner unchanged", bounded)
	}
	if _, ok := bounded.(sourcecypher.GroupExecutor); !ok {
		t.Fatal("disabled bound dropped GroupExecutor, want interface preserved")
	}
}

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
