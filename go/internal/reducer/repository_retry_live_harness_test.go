// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Shared harness for the #7285 live graph proofs: the real canonical writer
// and the real workload materializer against one Bolt backend.
//
// Run against an isolated Neo4j container (the owner's graph-proof backend):
//
//	docker run -d --name eshu-7285-neo4j -e NEO4J_AUTH=none \
//	  -p 127.0.0.1:38687:7687 neo4j:2026-community
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:38687 ESHU_NEO4J_DATABASE=neo4j \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j go test ./internal/reducer \
//	  -tags live_nornicdb_answer_truth -run 'TestLiveRepository' -count=1 -v
//
// The live-backend CI job (scripts/run-live-backend-tests.sh) exports the
// same variables for both NornicDB and Neo4j.
package reducer_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/retract"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// repoRetryLive is one test's connection, backend identity, and id prefix.
type repoRetryLive struct {
	exec    provenanceReplayExecutor
	backend string
	prefix  string
	// unguarded drops the stale-edge retract's graph reader, so the
	// keep-list DELETEs run unconditionally (the no-reader fallback).
	unguarded bool
	// retractDeletes counts the DEFINES / EXPOSES_ENDPOINT DELETE
	// statements the workload materializer sent to the graph.
	retractDeletes atomic.Int64
}

// liveEdgeReader is the stale-edge retract's graph read port over the live
// driver: a read-mode session, as the reducer's production graph reader.
type liveEdgeReader struct{ exec provenanceReplayExecutor }

func (r liveEdgeReader) Run(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	return r.exec.readRows(ctx, query, params)
}

// edgeReader returns the reader handleWorkloads wires, nil when unguarded.
func (l *repoRetryLive) edgeReader() retract.Reader {
	if l.unguarded {
		return nil
	}
	return liveEdgeReader{exec: l.exec}
}

// openRepoRetryLive connects with NoAuth, applies the production schema for
// the backend (the Repository id uniqueness constraint is load-bearing for
// the concurrent first-creation MERGE), and scopes every fixture id to a
// per-test nonce that is removed before and after the test.
func openRepoRetryLive(t *testing.T) *repoRetryLive {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Skip("ESHU_NEO4J_URI not set; skipping #7285 live repository-retry proof")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "nornic"
	}
	backend := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND"))
	if backend == "" {
		backend = strings.TrimSpace(os.Getenv("ESHU_GRAPH_BACKEND"))
	}
	if backend == "" {
		backend = "nornicdb"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open bolt driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify bolt connectivity %q: %v", uri, err)
	}
	live := &repoRetryLive{
		exec:    newProvenanceReplayExecutor(driver, database),
		backend: backend,
		prefix:  fmt.Sprintf("wd7285-%d", time.Now().UnixNano()),
	}
	schemaBackend := graph.SchemaBackendNornicDB
	if backend == "neo4j" {
		schemaBackend = graph.SchemaBackendNeo4j
	}
	if err := graph.EnsureSchemaWithBackend(ctx, live.exec, nil, schemaBackend); err != nil {
		t.Fatalf("apply %s schema: %v", backend, err)
	}
	live.cleanup(t)
	t.Cleanup(func() { live.cleanup(t) })
	return live
}

func (l *repoRetryLive) cleanup(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := l.exec.Execute(ctx, cypher.Statement{
		Cypher: `MATCH (n) WHERE n.id STARTS WITH $prefix OR n.path STARTS WITH $path_prefix
  OR n.uid STARTS WITH $prefix DETACH DELETE n`,
		Parameters: map[string]any{"prefix": l.prefix, "path_prefix": l.pathPrefix()},
	}); err != nil {
		t.Fatalf("clean #7285 fixture: %v", err)
	}
}

func (l *repoRetryLive) pathPrefix() string { return "/eshu-7285/" + l.prefix }

func (l *repoRetryLive) repoID(name string) string { return "repository:" + l.prefix + "-" + name }

func (l *repoRetryLive) repoPath(name string) string { return l.pathPrefix() + "/" + name }

// writerShapes returns the two production executor shapes: the atomic
// GroupExecutor (the Neo4j projector path) and the phase-group executor (the
// NornicDB projector path). Both must keep reducer edges across a retry.
func (l *repoRetryLive) writerShapes() map[string]*cypher.CanonicalNodeWriter {
	return l.instrumentedWriterShapes(nil)
}

// instrumentedWriterShapes is writerShapes with the writer's telemetry
// instruments wired, so a test can read the writer's own counters (#7324).
func (l *repoRetryLive) instrumentedWriterShapes(instruments *telemetry.Instruments) map[string]*cypher.CanonicalNodeWriter {
	phaseExecutor := storagenornicdb.PhaseGroupExecutor{
		Inner:                    l.exec,
		MaxStatements:            storagenornicdb.DefaultPhaseGroupStatements,
		DirectoryMaxStatements:   storagenornicdb.DefaultDirectoryPhaseStatements,
		FileMaxStatements:        storagenornicdb.DefaultFilePhaseStatements,
		EntityMaxStatements:      storagenornicdb.DefaultEntityPhaseStatements,
		EntityLabelMaxStatements: storagenornicdb.DefaultEntityLabelPhaseStatements(storagenornicdb.DefaultEntityPhaseStatements),
		EntityPhaseConcurrency:   storagenornicdb.DefaultEntityPhaseConcurrency(),
		DrainReader:              l.exec,
		RetractBatchSize:         storagenornicdb.DefaultCanonicalRetractBatchSize,
	}
	phaseWriter := cypher.NewCanonicalNodeWriter(phaseExecutor, 500, instruments)
	if l.backend != "neo4j" {
		phaseWriter = storagenornicdb.ConfigureCanonicalWriter(phaseWriter, storagenornicdb.DefaultWriterConfig())
	}
	return map[string]*cypher.CanonicalNodeWriter{
		"atomic_group": cypher.NewCanonicalNodeWriter(l.exec, 500, instruments),
		"phase_group":  phaseWriter,
	}
}

// repoFixture is one repository's canonical projection shape.
type repoFixture struct {
	name      string
	files     []string // repository-relative paths
	remoteURL string
}

var retryFixtureFiles = []string{
	"README.md", "go.mod", "src/main.go", "src/api/handler.go", "docs/guide.md", "deploy/app.yaml",
}

// materialization builds generation gen of fixture f. first=false is the
// retry shape: projector/service.go forces PreviousGenerationExists on every
// attempt >= 2, and canonical.BuildMaterialization derives FirstGeneration
// from it.
func (l *repoRetryLive) materialization(f repoFixture, gen string, first bool) canonical.CanonicalMaterialization {
	repoID, repoPath := l.repoID(f.name), l.repoPath(f.name)
	mat := canonical.CanonicalMaterialization{
		ScopeID:         "git-repository-scope:" + repoID,
		GenerationID:    gen,
		RepoID:          repoID,
		RepoPath:        repoPath,
		FirstGeneration: first,
		Repository: &canonical.RepositoryRow{
			RepoID: repoID, Name: f.name, Path: repoPath, LocalPath: repoPath,
			RemoteURL: f.remoteURL, RepoSlug: "eshu-hq/" + f.name, HasRemote: f.remoteURL != "",
		},
	}
	dirs := map[string]bool{}
	for _, rel := range f.files {
		parts := strings.Split(rel, "/")
		for depth := 0; depth < len(parts)-1; depth++ {
			dirRel := strings.Join(parts[:depth+1], "/")
			if dirs[dirRel] {
				continue
			}
			dirs[dirRel] = true
			parent := repoPath
			if depth > 0 {
				parent = repoPath + "/" + strings.Join(parts[:depth], "/")
			}
			mat.Directories = append(mat.Directories, canonical.DirectoryRow{
				Path: repoPath + "/" + dirRel, Name: parts[depth], ParentPath: parent, RepoID: repoID, Depth: depth,
			})
		}
		dirPath := repoPath
		if idx := strings.LastIndex(rel, "/"); idx >= 0 {
			dirPath = repoPath + "/" + rel[:idx]
		}
		mat.Files = append(mat.Files, canonical.FileRow{
			Path: repoPath + "/" + rel, RelativePath: rel, Name: parts[len(parts)-1],
			Language: "text", RepoID: repoID, DirPath: dirPath,
		})
	}
	return mat
}

func (f repoFixture) depthZeroDirectories() int {
	seen := map[string]bool{}
	for _, rel := range f.files {
		if idx := strings.Index(rel, "/"); idx >= 0 {
			seen[rel[:idx]] = true
		}
	}
	return len(seen)
}

func (l *repoRetryLive) write(ctx context.Context, t *testing.T, w *cypher.CanonicalNodeWriter, mat canonical.CanonicalMaterialization) {
	t.Helper()
	if err := w.Write(ctx, mat); err != nil {
		t.Fatalf("canonical write %s gen=%s first=%t: %v", mat.RepoID, mat.GenerationID, mat.FirstGeneration, err)
	}
}

// reducerExecutor adapts the Bolt executor to the reducer's CypherExecutor and
// CypherGroupExecutor contracts. ExecuteCypher is auto-commit, as the
// production reducerCypherExecutor dispatches it (an UNWIND ... DELETE must
// not move into a managed transaction, see nornicdb-pitfalls.md), and it
// retries driver-retryable errors such as DeadlockDetected, as the production
// sourcecypher.RetryingExecutor wrapping it does.
type reducerExecutor struct {
	exec           provenanceReplayExecutor
	retractDeletes *atomic.Int64
}

func (r reducerExecutor) ExecuteCypher(ctx context.Context, cypherText string, params map[string]any) error {
	if r.retractDeletes != nil && strings.Contains(cypherText, "DELETE rel") &&
		(strings.Contains(cypherText, "-[rel:DEFINES]->") || strings.Contains(cypherText, "-[rel:EXPOSES_ENDPOINT]->")) {
		r.retractDeletes.Add(1)
	}
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		err = r.exec.Execute(ctx, cypher.Statement{Cypher: cypherText, Parameters: params})
		if err == nil || !neo4jdriver.IsRetryable(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
	}
	return err
}

func (r reducerExecutor) ExecuteCypherGroup(ctx context.Context, statements []reducer.CypherGroupStatement) error {
	group := make([]cypher.Statement, 0, len(statements))
	for _, statement := range statements {
		group = append(group, cypher.Statement{Cypher: statement.Cypher, Parameters: statement.Parameters})
	}
	return r.exec.ExecuteGroup(ctx, group)
}

func (l *repoRetryLive) materializer() *reducer.WorkloadMaterializer {
	return reducer.NewWorkloadMaterializer(reducerExecutor{exec: l.exec, retractDeletes: &l.retractDeletes})
}

// relationshipTypeCounts returns every relationship incident to the
// Repository, by type, excluding the projector-owned CONTAINS and
// REPO_CONTAINS edges.
func (l *repoRetryLive) relationshipTypeCounts(ctx context.Context, t *testing.T, repoID string) map[string]int64 {
	t.Helper()
	rows, err := l.exec.readRows(ctx, `MATCH (r:Repository {id: $repo_id})-[rel]-()
WHERE NOT type(rel) IN ['CONTAINS', 'REPO_CONTAINS']
RETURN type(rel) AS rel_type, count(rel) AS count`, map[string]any{"repo_id": repoID})
	if err != nil {
		t.Fatalf("count relationships on %s: %v", repoID, err)
	}
	counts := map[string]int64{}
	for _, row := range rows {
		relType, _ := row["rel_type"].(string)
		count, _ := row["count"].(int64)
		counts[relType] = count
	}
	return counts
}

func (l *repoRetryLive) count(ctx context.Context, t *testing.T, query string, params map[string]any) int64 {
	t.Helper()
	count, err := l.exec.count(ctx, query, params)
	if err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return count
}

func (l *repoRetryLive) repositoryElementID(ctx context.Context, t *testing.T, repoID string) string {
	t.Helper()
	rows, err := l.exec.readRows(ctx, `MATCH (r:Repository {id: $repo_id}) RETURN elementId(r) AS element_id`,
		map[string]any{"repo_id": repoID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("read Repository %s element id: rows=%d err=%v", repoID, len(rows), err)
	}
	id, _ := rows[0]["element_id"].(string)
	return id
}

func (l *repoRetryLive) repositoryProperties(ctx context.Context, t *testing.T, repoID string) map[string]any {
	t.Helper()
	rows, err := l.exec.readRows(ctx, `MATCH (r:Repository {id: $repo_id}) RETURN properties(r) AS props`,
		map[string]any{"repo_id": repoID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("read Repository %s properties: rows=%d err=%v", repoID, len(rows), err)
	}
	props, _ := rows[0]["props"].(map[string]any)
	return props
}

func (l *repoRetryLive) run(ctx context.Context, t *testing.T, query string, params map[string]any) {
	t.Helper()
	if err := l.exec.Execute(ctx, cypher.Statement{Cypher: query, Parameters: params}); err != nil {
		t.Fatalf("run %q: %v", query, err)
	}
}
