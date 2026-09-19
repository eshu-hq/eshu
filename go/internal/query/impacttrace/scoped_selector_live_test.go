// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live scoped-grant answer-truth proof for #6786.
//
// ResolveWorkloadSelector backs the scoped grant for
// POST /api/v0/impact/trace-deployment-chain (and InvestigateDeploymentConfigInfluence).
// The scoped predicate used to be rendered as a multi-line
// `AND ( ... OR EXISTS {...} )` Cypher WHERE group appended to the
// selector's own `w.id = $service_name` / `w.name = $service_name` anchor.
// On the pinned NornicDB v1.3.3 image that shape is unreliable: it can
// silently drop the WHOLE WHERE, including the anchor, so a scoped caller's
// selector for one workload could resolve to an unrelated, ungranted
// workload -- "every selector resolves to the same workload" regardless of
// grant. The fix moved the grant decision into Go; these tests drive the
// real production function against a live backend with Eshu's real schema
// applied (NornicDB's read-predicate behavior differs materially without
// it) to prove the fix and guard the regression.
//
// Run against both pinned backends (schema and seed are applied fresh by
// each test run; the id prefix is unique per run):
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27880 \
//	  ESHU_LIVE_GRAPH_BACKEND=nornicdb ESHU_LIVE_GRAPH_DATABASE=nornic \
//	  go test ./internal/query/impacttrace -tags live_nornicdb_answer_truth \
//	  -run TestLiveResolveWorkloadSelector -count=1 -v
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27890 \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j ESHU_LIVE_GRAPH_DATABASE=neo4j \
//	  go test ./internal/query/impacttrace -tags live_nornicdb_answer_truth \
//	  -run TestLiveResolveWorkloadSelector -count=1 -v
package impacttrace

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// selectorLiveWriteMaxAttempts and selectorLiveWriteRetryBaseDelay mirror
// entity's liveWriteMaxAttempts/liveWriteRetryBaseDelay (that helper is
// unexported to its own package): they bound retries for a transient write
// conflict (e.g. NornicDB/Neo4j's Neo.TransientError.Transaction.Outdated) a
// live schema apply or seed write can hit when this package's and entity's
// live tests run as separate, concurrently-scheduled Go packages against the
// SAME live database (#6784 runs them that way in CI).
const (
	selectorLiveWriteMaxAttempts    = 5
	selectorLiveWriteRetryBaseDelay = 75 * time.Millisecond
)

// retrySelectorLiveWrite runs op up to selectorLiveWriteMaxAttempts times,
// retrying only when op's error is a Neo4j/NornicDB-classified transient
// error (neo4jdriver.IsRetryable). Any other error, or exhausting every
// attempt, returns immediately with that error.
func retrySelectorLiveWrite(ctx context.Context, op func() error) error {
	var lastErr error
	for attempt := 1; attempt <= selectorLiveWriteMaxAttempts; attempt++ {
		lastErr = op()
		if lastErr == nil {
			return nil
		}
		if !neo4jdriver.IsRetryable(lastErr) || attempt == selectorLiveWriteMaxAttempts {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * selectorLiveWriteRetryBaseDelay):
		}
	}
	return lastErr
}

const selectorLivePrefix = "scoped-selector-6786:"

// selectorLiveSeed creates two repositories -- one the test grants access
// to, one it does not -- three workloads (in-grant, out-of-grant with no
// relationship to the granted repository at all, and a name-collision
// workload whose own repo_id names the ungranted repository but which the
// granted repository DEFINES too), and two distinct workloads sharing one
// name to prove ambiguity detection survives the Go-side grant decision.
//
// wl-in and wl-out each carry a DEFINES edge from their OWN repo_id's
// repository, matching production materialization. #6786 review follow-up
// (F1): without repo-b DEFINES wl-out, OutOfGrantByIDReturnsEmpty could pass
// on a false green -- the workloadSelectorRowCypher `collect(DISTINCT dr.id)`
// finding zero defining ids either because it correctly excluded them, or
// because there was nothing to collect in the first place. Seeding the
// ungranted DEFINES edge forces the read to prove it actually surfaces an
// ungranted defining repository for querycontract.WorkloadGrantAdmitted to
// then correctly refuse.
var selectorLiveSeed = []string{
	`CREATE (:Repository {id: 'scoped-selector-6786:repo-a', name: 'repo-a'})`,
	`CREATE (:Repository {id: 'scoped-selector-6786:repo-b', name: 'repo-b'})`,
	`CREATE (:Workload {id: 'scoped-selector-6786:wl-in', name: 'svc-in', repo_id: 'scoped-selector-6786:repo-a'})`,
	`CREATE (:Workload {id: 'scoped-selector-6786:wl-out', name: 'svc-out', repo_id: 'scoped-selector-6786:repo-b'})`,
	`CREATE (:Workload {id: 'scoped-selector-6786:wl-collision', name: 'svc-collision', repo_id: 'scoped-selector-6786:repo-b'})`,
	`CREATE (:Workload {id: 'scoped-selector-6786:wl-amb-1', name: 'svc-ambiguous', repo_id: 'scoped-selector-6786:repo-a'})`,
	`CREATE (:Workload {id: 'scoped-selector-6786:wl-amb-2', name: 'svc-ambiguous', repo_id: 'scoped-selector-6786:repo-a'})`,
	selectorLiveEdge("Repository", "scoped-selector-6786:repo-a", "DEFINES", "Workload", "scoped-selector-6786:wl-in"),
	selectorLiveEdge("Repository", "scoped-selector-6786:repo-b", "DEFINES", "Workload", "scoped-selector-6786:wl-out"),
	selectorLiveEdge("Repository", "scoped-selector-6786:repo-a", "DEFINES", "Workload", "scoped-selector-6786:wl-collision"),
}

func selectorLiveEdge(fromLabel, fromID, relType, toLabel, toID string) string {
	return `MATCH (a:` + fromLabel + ` {id: '` + fromID + `'}) MATCH (b:` + toLabel + ` {id: '` + toID + `'}) CREATE (a)-[:` + relType + `]->(b)`
}

const selectorLiveCleanup = `MATCH (n) WHERE n.id STARTS WITH '` + selectorLivePrefix + `' DETACH DELETE n`

// selectorLiveReader is the test-only live GraphQuery for this file. The
// package cannot import the entity package's own live reader without an
// import cycle risk across the #6060 handler-family split, so this is a
// small, separately-declared copy of the same shape.
type selectorLiveReader struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (r selectorLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: r.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r selectorLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (r selectorLiveReader) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	err := retrySelectorLiveWrite(ctx, func() error {
		session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
		defer func() { _ = session.Close(ctx) }()
		result, runErr := session.Run(ctx, cypher, nil)
		if runErr != nil {
			return runErr
		}
		_, runErr = result.Consume(ctx)
		return runErr
	})
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
}

// selectorLiveSchemaExecutor adapts the driver to graph.CypherExecutor so
// EnsureSchemaWithBackendStrict can apply the real schema DDL.
type selectorLiveSchemaExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e selectorLiveSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	return retrySelectorLiveWrite(ctx, func() error {
		session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: e.database})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
		if err != nil {
			return err
		}
		_, err = result.Consume(ctx)
		return err
	})
}

// selectorLiveGraphBackend resolves the schema dialect and default database
// the same way entity's liveGraphBackend does (that helper is unexported to
// its own package and this package cannot import it).
func selectorLiveGraphBackend() (graph.SchemaBackend, string) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if backend == "neo4j" {
		if database == "" {
			database = "neo4j"
		}
		return graph.SchemaBackendNeo4j, database
	}
	if database == "" {
		database = "nornic"
	}
	return graph.SchemaBackendNornicDB, database
}

// selectorLiveFixture opens the driver, applies schema, seeds, and registers
// cleanup.
func selectorLiveFixture(t *testing.T) (selectorLiveReader, context.Context) {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend, database := selectorLiveGraphBackend()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	if err := graph.EnsureSchemaWithBackendStrict(ctx, selectorLiveSchemaExecutor{driver: driver, database: database}, nil, backend); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	reader := selectorLiveReader{driver: driver, database: database}
	reader.write(ctx, t, selectorLiveCleanup)
	for _, stmt := range selectorLiveSeed {
		reader.write(ctx, t, stmt)
	}
	t.Cleanup(func() { reader.write(context.Background(), t, selectorLiveCleanup) })

	return reader, ctx
}

func selectorScopedContext(ctx context.Context, allowedRepositoryIDs ...string) context.Context {
	return queryauth.ContextWithAuthContext(ctx, queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		AllowedRepositoryIDs: allowedRepositoryIDs,
	})
}

func TestLiveResolveWorkloadSelectorInGrantByID(t *testing.T) {
	reader, baseCtx := selectorLiveFixture(t)
	ctx := selectorScopedContext(baseCtx, "scoped-selector-6786:repo-a")

	got, err := ResolveWorkloadSelector(ctx, reader, "scoped-selector-6786:wl-in", nil, nil)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if got != "scoped-selector-6786:wl-in" {
		t.Fatalf("ResolveWorkloadSelector() = %q, want the in-grant workload id", got)
	}
}

// TestLiveResolveWorkloadSelectorOutOfGrantByIDReturnsEmpty is the
// #6786 answer-truth proof: an out-of-grant id selector must resolve to ""
// (not found), never to a DIFFERENT, in-grant workload -- the exact failure
// this package's retired Cypher-embedded grant predicate produced.
func TestLiveResolveWorkloadSelectorOutOfGrantByIDReturnsEmpty(t *testing.T) {
	reader, baseCtx := selectorLiveFixture(t)
	ctx := selectorScopedContext(baseCtx, "scoped-selector-6786:repo-a")

	got, err := ResolveWorkloadSelector(ctx, reader, "scoped-selector-6786:wl-out", nil, nil)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("ResolveWorkloadSelector() = %q, want not-found for an ungranted workload id (never a different, in-grant workload)", got)
	}
}

func TestLiveResolveWorkloadSelectorCollisionAdmittedByDefines(t *testing.T) {
	reader, baseCtx := selectorLiveFixture(t)
	ctx := selectorScopedContext(baseCtx, "scoped-selector-6786:repo-a")

	got, err := ResolveWorkloadSelector(ctx, reader, "scoped-selector-6786:wl-collision", nil, nil)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if got != "scoped-selector-6786:wl-collision" {
		t.Fatalf("ResolveWorkloadSelector() = %q, want the collision workload id (admitted via DEFINES)", got)
	}
}

func TestLiveResolveWorkloadSelectorByNameAmbiguous(t *testing.T) {
	reader, baseCtx := selectorLiveFixture(t)
	ctx := selectorScopedContext(baseCtx, "scoped-selector-6786:repo-a")

	_, err := ResolveWorkloadSelector(ctx, reader, "svc-ambiguous", nil, nil)
	if !errors.Is(err, ErrAmbiguousWorkloadSelector) {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want ambiguity", err)
	}
}

func TestLiveResolveWorkloadSelectorUnscopedUnchanged(t *testing.T) {
	reader, baseCtx := selectorLiveFixture(t)

	got, err := ResolveWorkloadSelector(baseCtx, reader, "scoped-selector-6786:wl-out", nil, nil)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if got != "scoped-selector-6786:wl-out" {
		t.Fatalf("ResolveWorkloadSelector() = %q, want the requested workload id for an unscoped caller", got)
	}
}
