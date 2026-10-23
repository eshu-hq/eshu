// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// #8795 live proofs of the legacy workload retirement against the real
// workload materializer on the pinned Neo4j: the retire path leaves the
// twin and its edges alone (P1), a second run deletes nothing (C1), and a
// retirement that races a materialization pass converges with the twin
// intact (C2). The retirement statement is Neo4j-only, so the file is
// registered for Neo4j.
package reducer_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/legacy/retire"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// liveRetireWriter is the retirement write port over the live driver. Like the
// production retrying executor it retries a transient error (a deadlock with a
// concurrent materialization pass) and reports the node-delete counter of the
// committed attempt.
type liveRetireWriter struct{ exec provenanceReplayExecutor }

func (w liveRetireWriter) ExecuteCypherCountingNodeDeletes(ctx context.Context, cypherText string, params map[string]any) (int64, error) {
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		collector := cypher.NewWriteCountsCollector()
		err := w.exec.Execute(cypher.WithWriteCountsCollector(ctx, collector), cypher.Statement{Cypher: cypherText, Parameters: params})
		if err == nil {
			var deleted int64
			for _, entry := range collector.Entries() {
				deleted += entry.Counters.NodesDeleted
			}
			return deleted, nil
		}
		lastErr = err
		var neo4jErr *neo4jdriver.Neo4jError
		if !errors.As(err, &neo4jErr) || !strings.HasPrefix(neo4jErr.Code, "Neo.TransientError") {
			return 0, err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, lastErr
}

type legacyRetireFixture struct {
	live       *repoRetryLive
	repoID     string
	name       string
	legacyID   string
	scopedID   string
	handleGen  func(gen string)
	seedLegacy func()
}

func openLegacyRetireFixture(t *testing.T) *legacyRetireFixture {
	t.Helper()
	live := openRepoRetryLive(t)
	if live.backend != "neo4j" {
		t.Skip("legacy workload retirement execute is Neo4j-only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanCancel()
		if err := live.exec.Execute(cleanCtx, cypher.Statement{
			Cypher:     `MATCH (n) WHERE n.repo_id STARTS WITH $repo_prefix DETACH DELETE n`,
			Parameters: map[string]any{"repo_prefix": live.repoID("")},
		}); err != nil {
			t.Errorf("clean legacy retirement fixture: %v", err)
		}
	})

	f := &legacyRetireFixture{live: live, repoID: live.repoID("legacy"), name: "cs-" + live.prefix}
	f.legacyID = "workload:" + f.name
	f.scopedID = "workload:" + f.repoID + ":" + f.name

	candidates := func() []reducer.WorkloadCandidate {
		return []reducer.WorkloadCandidate{{
			RepoID: f.repoID, RepoName: "legacy", WorkloadName: f.name,
			Classification: "service", Confidence: 0.95, Namespaces: []string{"prod"},
		}}
	}
	f.handleGen = func(gen string) {
		handler := reducer.WorkloadMaterializationHandler{
			FactLoader:           liveRepoFactLoader{repoIDs: []string{f.repoID}},
			InputLoader:          liveCandidateLoader{candidates: candidates()},
			Materializer:         live.materializer(),
			RepositoryEdgeReader: live.edgeReader(),
		}
		now := time.Now().UTC()
		if _, err := handler.Handle(ctx, reducercontract.Intent{
			IntentID: "intent-legacy-" + gen, ScopeID: "git-repository-scope:" + f.repoID,
			GenerationID: gen, SourceSystem: "git", Domain: reducer.DomainWorkloadMaterialization,
			Cause: "facts projected", EntityKeys: []string{f.repoID}, EnqueuedAt: now, AvailableAt: now,
			Status: reducer.IntentStatusPending,
		}); err != nil {
			t.Errorf("handle workload_materialization %s: %v", gen, err)
		}
	}
	f.seedLegacy = func() {
		stmts := []cypher.Statement{
			{
				Cypher:     `MERGE (w:Workload {id: $id}) SET w.name = $name, w.repo_id = $repo_id`,
				Parameters: map[string]any{"id": f.legacyID, "name": f.name, "repo_id": f.repoID},
			},
			{
				Cypher:     `MATCH (r:Repository {id: $repo_id}), (w:Workload {id: $id}) MERGE (r)-[:DEFINES {evidence_source: $src}]->(w)`,
				Parameters: map[string]any{"id": f.legacyID, "repo_id": f.repoID, "src": reducer.EvidenceSourceWorkloads},
			},
		}
		for _, stmt := range stmts {
			if err := live.exec.Execute(ctx, stmt); err != nil {
				t.Fatalf("seed legacy workload: %v", err)
			}
		}
	}
	if err := live.exec.Execute(ctx, cypher.Statement{
		Cypher:     `MERGE (r:Repository {id: $repo_id}) SET r.name = 'legacy'`,
		Parameters: map[string]any{"repo_id": f.repoID},
	}); err != nil {
		t.Fatalf("seed Repository node: %v", err)
	}
	f.handleGen("gen-1")
	f.seedLegacy()
	return f
}

func (f *legacyRetireFixture) runner() *retire.Runner {
	return &retire.Runner{
		Reader:           liveEdgeReader{exec: f.live.exec},
		Writer:           liveRetireWriter{exec: f.live.exec},
		BackendValidated: true,
	}
}

func (f *legacyRetireFixture) nodes(t *testing.T, id string) int64 {
	t.Helper()
	return f.live.count(context.Background(), t, `MATCH (w:Workload {id: $id}) RETURN count(w) AS count`, map[string]any{"id": id})
}

func (f *legacyRetireFixture) definesInto(t *testing.T, id string) int64 {
	t.Helper()
	return f.live.count(context.Background(), t,
		`MATCH (:Repository {id: $repo})-[r:DEFINES]->(:Workload {id: $id}) RETURN count(r) AS count`,
		map[string]any{"repo": f.repoID, "id": id})
}

func (f *legacyRetireFixture) settle(t *testing.T, what string, want int64, read func() int64) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		got := read()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s = %d, want %d after 30s of settling", what, got, want)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestLiveLegacyWorkloadRetirementP1AndC1 retires a real leftover next to a
// really materialized twin, then proves a second run is a no-op.
func TestLiveLegacyWorkloadRetirementP1AndC1(t *testing.T) {
	f := openLegacyRetireFixture(t)
	ctx := context.Background()
	runner := f.runner()

	if got := f.nodes(t, f.legacyID); got != 1 {
		t.Fatalf("legacy node = %d before retirement, want 1", got)
	}
	twinDefines := f.definesInto(t, f.scopedID)
	if twinDefines != 1 {
		t.Fatalf("twin DEFINES = %d, want 1 from the materializer", twinDefines)
	}

	before, err := legacyworkload.Detect(ctx, liveEdgeReader{exec: f.live.exec}, legacyworkload.Options{IDs: []string{f.legacyID}})
	if err != nil || before.WithScopedTwin() != 1 {
		t.Fatalf("readiness count before = %d (err %v), want 1: readiness is fail", before.WithScopedTwin(), err)
	}

	dry, err := runner.Run(ctx, retire.Request{DryRun: true, IDs: []string{f.legacyID}})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Eligible != 1 || dry.Deleted != 0 || len(dry.Candidates) != 1 || dry.Candidates[0].TwinID != f.scopedID {
		t.Fatalf("dry run = %+v, want the one leftover eligible with its twin %s", dry, f.scopedID)
	}
	if got := f.nodes(t, f.legacyID); got != 1 {
		t.Fatal("a dry run must not delete")
	}

	resp, err := runner.Run(ctx, retire.Request{IDs: []string{f.legacyID}, Reason: "post-cutover cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Deleted != 1 {
		t.Fatalf("execute = %+v, want deleted 1", resp)
	}
	f.settle(t, "legacy node", 0, func() int64 { return f.nodes(t, f.legacyID) })
	if got := f.nodes(t, f.scopedID); got != 1 {
		t.Fatalf("twin nodes = %d after retirement, want 1", got)
	}
	if got := f.definesInto(t, f.scopedID); got != twinDefines {
		t.Fatalf("twin DEFINES = %d after retirement, want %d unchanged", got, twinDefines)
	}

	after, err := legacyworkload.Detect(ctx, liveEdgeReader{exec: f.live.exec}, legacyworkload.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range after.Verdicts {
		if v.ID == f.legacyID {
			t.Fatal("the retired node is still detected")
		}
	}

	// C1: a second execute deletes nothing, and the clean-graph dry run lists
	// nothing for this fixture.
	again, err := runner.Run(ctx, retire.Request{IDs: []string{f.legacyID}, Reason: "post-cutover cleanup"})
	if err != nil || again.Deleted != 0 || again.Eligible != 0 {
		t.Fatalf("second run = %+v err=%v, want deleted 0", again, err)
	}
}

// TestLiveLegacyWorkloadRetirementC2RacesAMaterializationPass runs the
// retirement while the workload materializer re-runs for the same repository
// (its stale-DEFINES retract touches the leftover's DEFINES edge, so the two
// contend on the same node). Under -race the twin must stay intact and the edge
// counts converge; the leftover ends deleted.
func TestLiveLegacyWorkloadRetirementC2RacesAMaterializationPass(t *testing.T) {
	f := openLegacyRetireFixture(t)
	ctx := context.Background()
	runner := f.runner()

	for round := 0; round < 4; round++ {
		f.seedLegacy()
		var wg sync.WaitGroup
		var runErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, runErr = runner.Run(ctx, retire.Request{IDs: []string{f.legacyID}, Reason: fmt.Sprintf("race round %d", round)})
		}()
		go func() {
			defer wg.Done()
			f.handleGen(fmt.Sprintf("gen-race-%d", round))
		}()
		wg.Wait()
		if runErr != nil {
			t.Fatalf("round %d: retirement: %v", round, runErr)
		}
		// The pass and the retirement can finish in either order; a final run
		// is the idempotent convergence step an operator would do.
		if _, err := runner.Run(ctx, retire.Request{IDs: []string{f.legacyID}, Reason: "converge"}); err != nil {
			t.Fatalf("round %d: converge: %v", round, err)
		}
		f.settle(t, "legacy node", 0, func() int64 { return f.nodes(t, f.legacyID) })
		if got := f.nodes(t, f.scopedID); got != 1 {
			t.Fatalf("round %d: twin nodes = %d, want 1", round, got)
		}
		f.settle(t, "twin DEFINES", 1, func() int64 { return f.definesInto(t, f.scopedID) })
	}
}
