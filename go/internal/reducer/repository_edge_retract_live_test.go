// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package reducer_test

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Test B of #7285 (ruling B1). With the projector no longer deleting the
// Repository node, workload_materialization owns the retraction of its own
// stale DEFINES and repository-side EXPOSES_ENDPOINT edges. These run the real
// WorkloadMaterializationHandler against the live graph.

// liveRepoFactLoader serves one repository fact per repo for the scope, with
// the delta flag the collector stamps on incremental generations.
type liveRepoFactLoader struct {
	repoIDs []string
	delta   bool
}

func (l liveRepoFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	envelopes := make([]facts.Envelope, 0, len(l.repoIDs))
	for _, repoID := range l.repoIDs {
		payload := map[string]any{"graph_id": repoID, "name": repoID}
		if l.delta {
			payload["delta_generation"] = true
			payload["delta_relative_paths"] = []any{"src/main.go"}
		}
		envelopes = append(envelopes, facts.Envelope{
			FactID: "fact-" + repoID, FactKind: "repository", Payload: payload, ObservedAt: time.Now().UTC(),
		})
	}
	return envelopes, nil
}

type liveCandidateLoader struct{ candidates []reducer.WorkloadCandidate }

func (l liveCandidateLoader) LoadWorkloadProjectionInputs(
	context.Context, reducer.Intent,
) ([]reducer.WorkloadCandidate, map[string][]string, error) {
	return l.candidates, nil, nil
}

// handleWorkloads runs one workload_materialization intent for repo with the
// named workloads as candidates, each exposing /v1/<name>.
func (l *repoRetryLive) handleWorkloads(
	ctx context.Context, t *testing.T, repo, gen string, delta bool, names ...string,
) reducer.Result {
	t.Helper()
	result, err := l.handleWorkloadsErr(ctx, repo, gen, delta, names...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// handleWorkloadsErr is handleWorkloads for goroutines.
func (l *repoRetryLive) handleWorkloadsErr(
	ctx context.Context, repo, gen string, delta bool, names ...string,
) (reducer.Result, error) {
	repoID := l.repoID(repo)
	candidates := make([]reducer.WorkloadCandidate, 0, len(names))
	for _, name := range names {
		candidates = append(candidates, reducer.WorkloadCandidate{
			RepoID: repoID, RepoName: repo, WorkloadName: name, Classification: "service", Confidence: 0.95,
			APIEndpoints: []reducer.APIEndpointSignal{{Path: "/v1/" + name, Methods: []string{"GET"}}},
		})
	}
	handler := reducer.WorkloadMaterializationHandler{
		FactLoader:           liveRepoFactLoader{repoIDs: []string{repoID}, delta: delta},
		InputLoader:          liveCandidateLoader{candidates: candidates},
		Materializer:         l.materializer(),
		RepositoryEdgeReader: l.edgeReader(),
	}
	now := time.Now().UTC()
	result, err := handler.Handle(ctx, reducer.Intent{
		IntentID: fmt.Sprintf("intent-%s-%s", repo, gen), ScopeID: "git-repository-scope:" + repoID,
		GenerationID: gen, SourceSystem: "git", Domain: reducer.DomainWorkloadMaterialization,
		Cause: "facts projected", EntityKeys: []string{repoID}, EnqueuedAt: now, AvailableAt: now,
		Status: reducer.IntentStatusPending,
	})
	if err != nil {
		return result, fmt.Errorf("handle workload_materialization %s %s: %w", repo, gen, err)
	}
	return result, nil
}

// ownEdgeTargets returns the sorted target names of repo's DEFINES and
// repository-side EXPOSES_ENDPOINT edges carrying evidenceSource.
func (l *repoRetryLive) ownEdgeTargets(ctx context.Context, t *testing.T, repo, relType, evidenceSource string) []string {
	t.Helper()
	property := "w.name"
	if relType == "EXPOSES_ENDPOINT" {
		property = "w.path"
	}
	rows, err := l.exec.readRows(ctx, fmt.Sprintf(`MATCH (:Repository {id: $repo_id})-[rel:%s]->(w)
WHERE rel.evidence_source = $evidence_source
RETURN %s AS target`, relType, property),
		map[string]any{"repo_id": l.repoID(repo), "evidence_source": evidenceSource})
	if err != nil {
		t.Fatalf("read %s %s targets: %v", repo, relType, err)
	}
	targets := make([]string, 0, len(rows))
	for _, row := range rows {
		target, _ := row["target"].(string)
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

func assertTargets(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

// TestLiveRepositoryEdgeRetractKeepsOnlyCurrentCandidates proves B1: a full
// generation retracts exactly this repository's stale DEFINES and
// repository-side EXPOSES_ENDPOINT edges, keeps current ones, another
// writer's DEFINES on the same repository, and every edge of another
// repository (including a same-named workload); a delta generation never
// retracts; the zero-candidate path retracts all of this repository's own
// edges; and the retract is idempotent. It runs the production guarded path
// (read current targets, delete only stale ids) and the no-reader fallback
// (keep-list DELETE); the guarded path must send no DELETE when nothing is
// stale (review F1, NornicDB#296).
func TestLiveRepositoryEdgeRetractKeepsOnlyCurrentCandidates(t *testing.T) {
	for _, mode := range []string{"guarded", "unguarded"} {
		t.Run(mode, func(t *testing.T) {
			live := openRepoRetryLive(t)
			live.unguarded = mode == "unguarded"
			assertRepositoryEdgeRetract(t, live)
		})
	}
}

func assertRepositoryEdgeRetract(t *testing.T, live *repoRetryLive) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	writer := live.writerShapes()["atomic_group"]
	live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-1", true))
	live.write(ctx, t, writer, live.materialization(retryFixtureOther, "gen-1", true))
	live.run(ctx, t, `MATCH (r:Repository {id: $repo}) MERGE (w:Workload {id: $prefix + '-foreign'})
SET w.name = 'foreign' MERGE (r)-[rel:DEFINES]->(w) SET rel.evidence_source = 'other/writer'`,
		map[string]any{"repo": live.repoID(retryFixtureRepo.name), "prefix": live.prefix})

	repo, other := retryFixtureRepo.name, retryFixtureOther.name
	const own = reducer.EvidenceSourceWorkloads
	live.handleWorkloads(ctx, t, repo, "gen-1", false, "api", "billing", "worker")
	live.handleWorkloads(ctx, t, other, "gen-1", false, "billing")
	assertTargets(t, "gen-1 own DEFINES", live.ownEdgeTargets(ctx, t, repo, "DEFINES", own), "api", "billing", "worker")

	// Full generation 2 keeps only "api".
	for attempt := 1; attempt <= 2; attempt++ { // the second pass proves idempotency
		before := live.retractDeletes.Load()
		live.handleWorkloads(ctx, t, repo, "gen-2", false, "api")
		sent := live.retractDeletes.Load() - before
		label := fmt.Sprintf("gen-2 attempt %d", attempt)
		assertTargets(t, label+" own DEFINES", live.ownEdgeTargets(ctx, t, repo, "DEFINES", own), "api")
		assertTargets(t, label+" own EXPOSES_ENDPOINT", live.ownEdgeTargets(ctx, t, repo, "EXPOSES_ENDPOINT", own), "/v1/api")
		assertTargets(t, label+" other-writer DEFINES", live.ownEdgeTargets(ctx, t, repo, "DEFINES", "other/writer"), "foreign")
		assertTargets(t, label+" other repository DEFINES", live.ownEdgeTargets(ctx, t, other, "DEFINES", own), "billing")
		assertTargets(t, label+" other repository EXPOSES_ENDPOINT", live.ownEdgeTargets(ctx, t, other, "EXPOSES_ENDPOINT", own), "/v1/billing")
		// Attempt 1 has stale billing/worker edges; attempt 2 is steady
		// state. The guarded path sends a DELETE only for real stale edges.
		want := int64(2)
		if !live.unguarded && attempt == 2 {
			want = 0
		}
		if sent != want {
			t.Fatalf("%s sent %d retract DELETE statements, want %d", label, sent, want)
		}
	}

	// A delta generation reads partial facts: it must never retract, even
	// with zero candidates.
	live.handleWorkloads(ctx, t, repo, "gen-3", true)
	assertTargets(t, "delta gen-3 own DEFINES", live.ownEdgeTargets(ctx, t, repo, "DEFINES", own), "api")

	// Full generation with zero candidates: every own edge goes, nothing else.
	live.handleWorkloads(ctx, t, repo, "gen-4", false)
	assertTargets(t, "zero-candidate own DEFINES", live.ownEdgeTargets(ctx, t, repo, "DEFINES", own))
	assertTargets(t, "zero-candidate own EXPOSES_ENDPOINT", live.ownEdgeTargets(ctx, t, repo, "EXPOSES_ENDPOINT", own))
	assertTargets(t, "zero-candidate other-writer DEFINES", live.ownEdgeTargets(ctx, t, repo, "DEFINES", "other/writer"), "foreign")
	assertTargets(t, "zero-candidate other repository DEFINES", live.ownEdgeTargets(ctx, t, other, "DEFINES", own), "billing")
}
