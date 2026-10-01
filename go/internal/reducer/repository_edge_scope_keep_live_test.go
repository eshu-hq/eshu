// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package reducer_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// Proof 2 of the #7285 retract-scope ruling (arbiter option B). Two
// workload_materialization intents reach the same scope generation: one keyed
// to the scope's repository, one keyed to a foreign repository (a
// repo_dependency or deployment_mapping replay). Both run the real
// CorrelatedWorkloadProjectionInputLoader over repository and file facts.
// Whatever their order, and when they race, the repository keeps its DEFINES
// and repository-side EXPOSES_ENDPOINT edges, because the retract keep-list is
// the scope generation's admitted set rather than the intent's filtered one.

// liveScopeFactLoader serves one scope generation's facts.
type liveScopeFactLoader struct{ envelopes []facts.Envelope }

func (l liveScopeFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

// scopeKeepFacts is repo's repository fact plus, with a workload, an ArgoCD
// Application declaring a Deployment and an OpenAPI spec exposing /orders.
func (l *repoRetryLive) scopeKeepFacts(repo string, withWorkload bool) []facts.Envelope {
	repoID, now := l.repoID(repo), time.Now().UTC()
	envelopes := []facts.Envelope{{
		FactID: "fact-repo-" + repo, FactKind: "repository", ObservedAt: now,
		Payload: map[string]any{"graph_id": repoID, "name": repo},
	}}
	if !withWorkload {
		return envelopes
	}
	return append(envelopes,
		facts.Envelope{FactID: "fact-app-" + repo, FactKind: "file", ObservedAt: now, Payload: map[string]any{
			"repo_id": repoID, "relative_path": "deploy/application.yaml", "artifact_type": "argocd", "language": "yaml",
			"parsed_file_data": map[string]any{"k8s_resources": []any{map[string]any{"kind": "Deployment", "namespace": "prod"}}},
		}},
		facts.Envelope{FactID: "fact-spec-" + repo, FactKind: "file", ObservedAt: now, Payload: map[string]any{
			"repo_id": repoID, "relative_path": "specs/openapi.yaml",
			"parsed_file_data": map[string]any{"source": "openapi: 3.1.0\ninfo:\n  version: v1\n" +
				"paths:\n  /orders:\n    get:\n      operationId: listOrders\n"},
		}},
	)
}

// handleScopeIntent runs one workload_materialization intent for repo's scope
// generation gen through the real correlated input loader.
func (l *repoRetryLive) handleScopeIntent(ctx context.Context, repo, gen string, withWorkload bool, keys ...string) error {
	factLoader := liveScopeFactLoader{envelopes: l.scopeKeepFacts(repo, withWorkload)}
	handler := reducer.WorkloadMaterializationHandler{
		FactLoader:           factLoader,
		InputLoader:          reducer.CorrelatedWorkloadProjectionInputLoader{FactLoader: factLoader},
		Materializer:         l.materializer(),
		RepositoryEdgeReader: l.edgeReader(),
	}
	now := time.Now().UTC()
	repoID := l.repoID(repo)
	_, err := handler.Handle(ctx, reducer.Intent{
		IntentID: fmt.Sprintf("intent-%s-%s-%v", repo, gen, keys), ScopeID: "git-repository-scope:" + repoID,
		GenerationID: gen, SourceSystem: "git", Domain: reducer.DomainWorkloadMaterialization,
		Cause: "facts projected", EntityKeys: keys, EnqueuedAt: now, AvailableAt: now,
		Status: reducer.IntentStatusPending,
	})
	if err != nil {
		return fmt.Errorf("handle workload_materialization %s %s keys %v: %w", repo, gen, keys, err)
	}
	return nil
}

// scopeKeepEdges reads repo's own DEFINES and repository-side
// EXPOSES_ENDPOINT targets as one comparable string.
func (l *repoRetryLive) scopeKeepEdges(ctx context.Context, t *testing.T, repo string) string {
	t.Helper()
	own := reducer.EvidenceSourceWorkloads
	return fmt.Sprintf("DEFINES=%v EXPOSES_ENDPOINT=%v",
		l.ownEdgeTargets(ctx, t, repo, "DEFINES", own), l.ownEdgeTargets(ctx, t, repo, "EXPOSES_ENDPOINT", own))
}

// TestLiveRepositoryEdgeRetractKeepsScopeWorkloadsInEveryIntentOrder runs the
// matching and foreign intents in both orders and racing (10 trials), each on
// a fresh repository, and requires one final graph: the repository DEFINES
// its workload and exposes /orders. A generation that drops the workload,
// reached only by a foreign-keyed intent, still retracts both edges.
func TestLiveRepositoryEdgeRetractKeepsScopeWorkloadsInEveryIntentOrder(t *testing.T) {
	for _, mode := range []string{"guarded", "unguarded"} {
		t.Run(mode, func(t *testing.T) {
			live := openRepoRetryLive(t)
			live.unguarded = mode == "unguarded"
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				// Workload and Endpoint ids carry no nonce; their repo_id does.
				if err := live.exec.Execute(ctx, cypher.Statement{
					Cypher:     `MATCH (n) WHERE n.repo_id STARTS WITH $repo_prefix DETACH DELETE n`,
					Parameters: map[string]any{"repo_prefix": live.repoID("")},
				}); err != nil {
					t.Errorf("clean scope-keep workloads: %v", err)
				}
			})
			assertScopeKeepOrders(t, live)
		})
	}
}

func assertScopeKeepOrders(t *testing.T, live *repoRetryLive) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	writer := live.writerShapes()["atomic_group"]
	foreign := "repo:" + live.repoID("scope-keep-foreign")
	legs := map[string]func(repo string) error{
		"matching then foreign": func(repo string) error {
			if err := live.handleScopeIntent(ctx, repo, "gen-1", true, "workload:"+live.repoID(repo)); err != nil {
				return err
			}
			return live.handleScopeIntent(ctx, repo, "gen-1", true, foreign)
		},
		"foreign then matching": func(repo string) error {
			if err := live.handleScopeIntent(ctx, repo, "gen-1", true, foreign); err != nil {
				return err
			}
			return live.handleScopeIntent(ctx, repo, "gen-1", true, "workload:"+live.repoID(repo))
		},
	}
	for trial := 0; trial < 10; trial++ {
		legs[fmt.Sprintf("race %02d", trial)] = func(repo string) error {
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i, keys := range []string{"workload:" + live.repoID(repo), foreign} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[i] = live.handleScopeIntent(ctx, repo, "gen-1", true, keys)
				}()
			}
			wg.Wait()
			if errs[0] != nil {
				return errs[0]
			}
			return errs[1]
		}
	}
	index := 0
	for name, leg := range legs {
		repo := fmt.Sprintf("scope-keep-%02d", index)
		index++
		live.write(ctx, t, writer, live.materialization(repoFixture{name: repo, files: retryFixtureFiles}, "gen-1", true))
		if err := leg(repo); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := fmt.Sprintf("DEFINES=[%s] EXPOSES_ENDPOINT=[/orders]", repo)
		if got := live.scopeKeepEdges(ctx, t, repo); got != want {
			t.Fatalf("%s: final repository edges %s, want %s: the retract kept only the entity-filtered projection", name, got, want)
		}
		if name != "matching then foreign" {
			continue
		}
		// Generation 2 drops the workload; only a foreign-keyed intent runs.
		if err := live.handleScopeIntent(ctx, repo, "gen-2", false, foreign); err != nil {
			t.Fatalf("disappearance: %v", err)
		}
		if got := live.scopeKeepEdges(ctx, t, repo); got != "DEFINES=[] EXPOSES_ENDPOINT=[]" {
			t.Fatalf("disappearance reached only by a foreign-keyed intent left %s, want both edges retracted", got)
		}
	}
}
