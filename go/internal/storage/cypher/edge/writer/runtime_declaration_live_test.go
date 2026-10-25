// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package writer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/runtime/declaration"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1"
)

// The live proofs below drive the production runtime_declaration_projection
// handler over the production RuntimeDeclarationWriter against the pinned
// Neo4j (#8816). The live-backend CI job runs this file on Neo4j only
// (ESHU_NEO4J_URI, ESHU_LIVE_GRAPH_BACKEND, ESHU_LIVE_GRAPH_DATABASE, auth
// disabled). Locally:
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<port> ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test -race -tags live_nornicdb_answer_truth ./internal/storage/cypher/edge/writer \
//	  -run '^TestBoltRuntimeDeclaration' -count=1 -v
//
// Each test applies the production RuntimeDeclaration and Repository DDL
// first, so the composite identity constraint is what enforces one node per
// key.

type liveRuntimeFactLoader struct{ envelopes []facts.Envelope }

func (l liveRuntimeFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

type liveDecl struct {
	path  string
	line  int
	order int
	rt    string
	raw   string
	major *int
}

func liveMajor(v int) *int { return &v }

func sourceStatement(cypher string) sourcecypher.Statement {
	return sourcecypher.Statement{Cypher: cypher, Parameters: map[string]any{}}
}

func liveRuntimeFacts(t *testing.T, repoID string, delta bool, changed, deleted []string, decls ...liveDecl) []facts.Envelope {
	t.Helper()
	repo := map[string]any{"repo_id": repoID}
	if delta {
		repo["delta_generation"] = true
		repo["delta_relative_paths"] = changed
		repo["delta_deleted_relative_paths"] = deleted
	}
	out := []facts.Envelope{{FactKind: "repository", Payload: repo}}
	for _, d := range decls {
		specKind := runtimedeclarationv1.SpecKindExact
		if d.major == nil {
			specKind = runtimedeclarationv1.SpecKindAlias
		}
		payload, err := factschema.EncodeRuntimeDeclarationVersion(runtimedeclarationv1.Version{
			RepoID: repoID, RelativePath: d.path, ScopeDir: ".", LineNumber: d.line, OrderIndex: d.order,
			SourceKind: "tool_versions", Runtime: d.rt, RawSpec: d.raw, SpecKind: specKind, VersionMajor: d.major,
		})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		out = append(out, facts.Envelope{
			FactKind: facts.RuntimeDeclarationVersionFactKind, SchemaVersion: facts.RuntimeDeclarationSchemaVersionV1, Payload: payload,
		})
	}
	return out
}

func openRuntimeDeclarationLive(t *testing.T) (*boltRetractTestRunner, *RuntimeDeclarationWriter) {
	t.Helper()
	runner := openLiveVerifyRunner(t)
	stmts, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("schema statements: %v", err)
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt, ":RuntimeDeclaration)") || strings.Contains(stmt, "CONSTRAINT repository_id ") {
			if err := runner.runCypherSingle(context.Background(), sourceStatement(stmt)); err != nil {
				t.Fatalf("apply schema %q: %v", stmt, err)
			}
		}
	}
	return runner, NewRuntimeDeclarationWriter(&boltTestExecutor{runner: runner}, 0)
}

func liveProject(t *testing.T, w *RuntimeDeclarationWriter, generation string, envelopes []facts.Envelope) {
	t.Helper()
	h := declaration.Handler{
		FactLoader:           liveRuntimeFactLoader{envelopes: envelopes},
		Writer:               w,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	if _, err := h.Handle(context.Background(), reducercontract.Intent{
		IntentID: "intent-" + generation, ScopeID: "scope", GenerationID: generation,
		Domain: reducercontract.DomainRuntimeDeclarationProjection, AttemptCount: 1,
	}); err != nil {
		t.Fatalf("Handle(%s): %v", generation, err)
	}
}

// liveCounts returns (nodes, edges, paths) for one repository: the
// declaration nodes, the DECLARES_RUNTIME edges from its Repository, and the
// distinct relative paths those nodes carry.
func liveCounts(t *testing.T, runner *boltRetractTestRunner, repoID string) (int64, int64, string) {
	t.Helper()
	ctx := context.Background()
	nodes, err := boltCount(ctx, runner, `MATCH (d:RuntimeDeclaration {repo_id: $repo_id}) RETURN count(d) AS count`, map[string]any{"repo_id": repoID})
	if err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	edges, err := boltCount(ctx, runner, `MATCH (:Repository {id: $repo_id})-[r:DECLARES_RUNTIME]->(:RuntimeDeclaration) RETURN count(r) AS count`, map[string]any{"repo_id": repoID})
	if err != nil {
		t.Fatalf("count edges: %v", err)
	}
	rows, err := runner.runCypher(ctx, `MATCH (d:RuntimeDeclaration {repo_id: $repo_id}) WITH DISTINCT d.relative_path AS p ORDER BY p RETURN collect(p) AS paths`, map[string]any{"repo_id": repoID})
	if err != nil || len(rows) == 0 {
		t.Fatalf("paths: %v", err)
	}
	return nodes, edges, fmt.Sprint(rows[0]["paths"])
}

func liveCleanup(t *testing.T, runner *boltRetractTestRunner, repoIDs ...string) {
	t.Helper()
	t.Cleanup(func() {
		if err := boltWriteStatement(context.Background(), runner,
			`MATCH (n) WHERE (n:Repository AND n.id IN $ids) OR (n:RuntimeDeclaration AND n.repo_id IN $ids) DETACH DELETE n`,
			map[string]any{"ids": repoIDs}); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
}

// TestBoltRuntimeDeclarationProjectionIsIdempotentAndRetracts proves, on a
// real backend: two runs of one generation give the same counts; a version
// edited in place keeps one node; a full generation without a file retracts
// that file's declarations; a delta generation that deletes one file removes
// only that file's declarations and keeps the unchanged file's.
func TestBoltRuntimeDeclarationProjectionIsIdempotentAndRetracts(t *testing.T) {
	runner, w := openRuntimeDeclarationLive(t)
	repoID := fmt.Sprintf("repository:8816-live-%d", time.Now().UnixNano())
	liveCleanup(t, runner, repoID)

	full := liveRuntimeFacts(t, repoID, false, nil, nil,
		liveDecl{".tool-versions", 1, 0, "node", "20.11.1", liveMajor(20)},
		liveDecl{".tool-versions", 1, 1, "node", "18.19.0", liveMajor(18)},
		liveDecl{".python-version", 1, 0, "python", "3.12.1", liveMajor(3)},
		liveDecl{"svc/.nvmrc", 1, 0, "node", "lts/iron", nil},
	)
	liveProject(t, w, "gen-1", full)
	n1, e1, _ := liveCounts(t, runner, repoID)
	liveProject(t, w, "gen-1", full)
	n2, e2, paths := liveCounts(t, runner, repoID)
	if n1 != 4 || e1 != 4 || n2 != n1 || e2 != e1 {
		t.Fatalf("idempotency: run1=(%d,%d) run2=(%d,%d), want (4,4) twice; paths=%s", n1, e1, n2, e2, paths)
	}
	// Ambiguous case: the lts/iron alias is a node with no version_major, so
	// the version-threshold read counts only the real 18.19.0 pin. A zero-filled
	// major would put the alias below every threshold.
	below20, err := boltCount(context.Background(), runner,
		`MATCH (d:RuntimeDeclaration) WHERE d.repo_id = $repo_id AND d.runtime = 'node' AND d.version_major < 20 RETURN count(d) AS count`,
		map[string]any{"repo_id": repoID})
	if err != nil || below20 != 1 {
		t.Fatalf("node below 20 = %d (err %v), want 1 (18.19.0 only; the alias has no major)", below20, err)
	}
	aliasNoMajor, err := boltCount(context.Background(), runner,
		`MATCH (d:RuntimeDeclaration {repo_id: $repo_id, relative_path: 'svc/.nvmrc'}) WHERE d.version_major IS NULL AND d.spec_kind = 'alias' AND d.raw_spec = 'lts/iron' RETURN count(d) AS count`,
		map[string]any{"repo_id": repoID})
	if err != nil || aliasNoMajor != 1 {
		t.Fatalf("alias node without version_major = %d (err %v), want 1", aliasNoMajor, err)
	}

	edited := liveRuntimeFacts(t, repoID, false, nil, nil,
		liveDecl{".tool-versions", 1, 0, "node", "22.1.0", liveMajor(22)},
		liveDecl{".tool-versions", 1, 1, "node", "18.19.0", liveMajor(18)},
		liveDecl{".python-version", 1, 0, "python", "3.12.1", liveMajor(3)},
	)
	liveProject(t, w, "gen-2", edited)
	if n, e, p := liveCounts(t, runner, repoID); n != 3 || e != 3 || p != "[.python-version .tool-versions]" {
		t.Fatalf("full generation without svc/.nvmrc: (%d,%d) paths=%s, want (3,3) and the nvmrc retracted", n, e, p)
	}
	major, err := boltCount(context.Background(), runner,
		`MATCH (d:RuntimeDeclaration {repo_id: $repo_id, relative_path: '.tool-versions', line_number: 1, runtime: 'node', order_index: 0}) RETURN d.version_major AS count`,
		map[string]any{"repo_id": repoID})
	if err != nil || major != 22 {
		t.Fatalf("in-place edit: version_major = %d (err %v), want 22", major, err)
	}

	deltaDelete := liveRuntimeFacts(t, repoID, true, []string{"main.go"}, []string{".python-version"})
	liveProject(t, w, "gen-3", deltaDelete)
	if n, e, p := liveCounts(t, runner, repoID); n != 2 || e != 2 || p != "[.tool-versions]" {
		t.Fatalf("delta deleting .python-version: (%d,%d) paths=%s, want (2,2) and .tool-versions kept", n, e, p)
	}
}

// TestBoltRuntimeDeclarationConcurrentRepositoriesStayIsolated runs the
// handler for several repositories at once, each with the same pin-file
// paths, in two overlapping rounds where the second round deletes one file
// per repository on a delta. Every repository must end with exactly its own
// declarations: a retract never crosses repositories and concurrent MERGEs
// on distinct keys never collapse or duplicate a node.
func TestBoltRuntimeDeclarationConcurrentRepositoriesStayIsolated(t *testing.T) {
	runner, w := openRuntimeDeclarationLive(t)
	const repos = 8
	run := time.Now().UnixNano()
	ids := make([]string, repos)
	for i := range ids {
		ids[i] = fmt.Sprintf("repository:8816-race-%d-%d", run, i)
	}
	liveCleanup(t, runner, ids...)

	decls := []liveDecl{
		{".tool-versions", 1, 0, "node", "20.11.1", liveMajor(20)},
		{".tool-versions", 2, 0, "python", "3.12.1", liveMajor(3)},
		{".nvmrc", 1, 0, "node", "18", liveMajor(18)},
	}
	round := func(name string, envFor func(repoID string) []facts.Envelope) {
		var wg sync.WaitGroup
		for _, repoID := range ids {
			wg.Add(1)
			go func(repoID string) {
				defer wg.Done()
				h := declaration.Handler{
					FactLoader:           liveRuntimeFactLoader{envelopes: envFor(repoID)},
					Writer:               w,
					PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
				}
				if _, err := h.Handle(context.Background(), reducercontract.Intent{
					IntentID: name + repoID, ScopeID: repoID, GenerationID: name,
					Domain: reducercontract.DomainRuntimeDeclarationProjection, AttemptCount: 1,
				}); err != nil {
					t.Errorf("%s Handle(%s): %v", name, repoID, err)
				}
			}(repoID)
		}
		wg.Wait()
	}
	round("gen-1", func(repoID string) []facts.Envelope { return liveRuntimeFacts(t, repoID, false, nil, nil, decls...) })
	round("gen-1-replay", func(repoID string) []facts.Envelope { return liveRuntimeFacts(t, repoID, false, nil, nil, decls...) })
	for _, repoID := range ids {
		if n, e, p := liveCounts(t, runner, repoID); n != 3 || e != 3 {
			t.Fatalf("after concurrent full rounds %s: (%d,%d) paths=%s, want (3,3)", repoID, n, e, p)
		}
	}
	// Even repositories delete .nvmrc; odd ones change an unrelated file. A
	// retract that leaked across repositories would strip the odd
	// repositories' .nvmrc, which every repository declares at the same path.
	deletes := func(i int) bool { return i%2 == 0 }
	index := make(map[string]int, len(ids))
	for i, repoID := range ids {
		index[repoID] = i
	}
	round("gen-2", func(repoID string) []facts.Envelope {
		if deletes(index[repoID]) {
			return liveRuntimeFacts(t, repoID, true, nil, []string{".nvmrc"})
		}
		return liveRuntimeFacts(t, repoID, true, []string{"main.go"}, nil)
	})
	for i, repoID := range ids {
		wantN, wantPaths := int64(3), "[.nvmrc .tool-versions]"
		if deletes(i) {
			wantN, wantPaths = 2, "[.tool-versions]"
		}
		if n, e, p := liveCounts(t, runner, repoID); n != wantN || e != wantN || p != wantPaths {
			t.Fatalf("after concurrent deltas %s: (%d,%d) paths=%s, want (%d,%d) %s", repoID, n, e, p, wantN, wantN, wantPaths)
		}
	}
}
