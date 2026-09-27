// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package reducer_test

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
)

// #7306 live graph truth. A deployable_unit_correlation intent keyed to the
// scope's repository and one keyed to a foreign repository reach the same
// scope generation. Through the real handler and the real EdgeWriter, in both
// orders and racing, the repository ends with exactly its admitted
// CORRELATES_DEPLOYABLE_UNIT edge: the stale edge is retracted and the foreign
// intent deletes nothing, because this handler's retract and write sets are
// both the key-matched repositories (unlike #7304's scope-wide retract over a
// filtered keep-list).

// liveDUResolvedLoader serves one DEPLOYS_FROM from the deployment repository
// to the application repository.
type liveDUResolvedLoader struct{ appRepoID, deployRepoID string }

func (l liveDUResolvedLoader) GetResolvedRelationships(context.Context, string) ([]relationships.ResolvedRelationship, error) {
	return []relationships.ResolvedRelationship{{
		SourceRepoID: l.deployRepoID, TargetRepoID: l.appRepoID,
		RelationshipType: relationships.RelDeploysFrom, Confidence: 0.94,
		Details: map[string]any{"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDAppSource)}},
	}}, nil
}

// duLiveFacts is the application repository's fact plus, when deployable, the
// Dockerfile that makes it a deployable-unit candidate.
func (l *repoRetryLive) duLiveFacts(repo string, deployable bool) []facts.Envelope {
	repoID, now := l.repoID(repo), time.Now().UTC()
	envelopes := []facts.Envelope{{
		FactID: "fact-repo-" + repo, FactKind: "repository", ObservedAt: now,
		Payload: map[string]any{"graph_id": repoID, "name": repo},
	}}
	if deployable {
		envelopes = append(envelopes, facts.Envelope{
			FactID: "fact-dockerfile-" + repo, FactKind: "file", ObservedAt: now, Payload: map[string]any{
				"repo_id": repoID, "language": "dockerfile", "relative_path": "Dockerfile",
				"parsed_file_data": map[string]any{"dockerfile_stages": []any{map[string]any{"name": "runtime"}}},
			},
		})
	}
	return envelopes
}

// handleDUIntent runs one deployable_unit_correlation intent for repo's scope
// generation gen through the real handler and EdgeWriter.
func (l *repoRetryLive) handleDUIntent(ctx context.Context, repo, gen string, deployable bool, key string) error {
	handler := reducer.DeployableUnitCorrelationHandler{
		FactLoader:     liveScopeFactLoader{envelopes: l.duLiveFacts(repo, deployable)},
		ResolvedLoader: liveDUResolvedLoader{appRepoID: l.repoID(repo), deployRepoID: l.repoID(repo + "-deploy")},
		EdgeWriter:     edgewriter.NewEdgeWriter(l.exec, 0),
	}
	now := time.Now().UTC()
	_, err := handler.Handle(ctx, reducer.Intent{
		IntentID: fmt.Sprintf("intent-%s-%s-%s", repo, gen, key), ScopeID: "git-repository-scope:" + l.repoID(repo),
		GenerationID: gen, SourceSystem: "git", Domain: reducer.DomainDeployableUnitCorrelation,
		Cause: "facts committed", EntityKeys: []string{key}, EnqueuedAt: now, AvailableAt: now,
		Status: reducer.IntentStatusPending,
	})
	if err != nil {
		return fmt.Errorf("handle deployable_unit_correlation %s %s key %s: %w", repo, gen, key, err)
	}
	return nil
}

// seedDURepos creates repo, its deployment repository and a retired
// deployment repository, plus a stale correlation edge to the retired one.
func (l *repoRetryLive) seedDURepos(ctx context.Context, t *testing.T, repo string) {
	t.Helper()
	for _, id := range []string{l.repoID(repo), l.repoID(repo + "-deploy"), l.repoID(repo + "-retired")} {
		if err := l.exec.Execute(ctx, cypher.Statement{
			Cypher: `MERGE (:Repository {id: $id})`, Parameters: map[string]any{"id": id},
		}); err != nil {
			t.Fatalf("seed repository %s: %v", id, err)
		}
	}
	if err := l.exec.Execute(ctx, cypher.Statement{
		Cypher: `MATCH (a:Repository {id: $app}), (r:Repository {id: $retired})
MERGE (a)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(r) SET rel.evidence_source = $evidence_source`,
		Parameters: map[string]any{
			"app": l.repoID(repo), "retired": l.repoID(repo + "-retired"),
			"evidence_source": "reducer/deployable-unit-correlation",
		},
	}); err != nil {
		t.Fatalf("seed stale correlation edge: %v", err)
	}
}

// duEdgeTargets returns the sorted target ids of repo's correlation edges.
func (l *repoRetryLive) duEdgeTargets(ctx context.Context, t *testing.T, repo string) []string {
	t.Helper()
	rows, err := l.exec.readRows(ctx, `MATCH (:Repository {id: $repo_id})-[rel:CORRELATES_DEPLOYABLE_UNIT]->(d:Repository)
WHERE rel.evidence_source = 'reducer/deployable-unit-correlation'
RETURN d.id AS target`, map[string]any{"repo_id": l.repoID(repo)})
	if err != nil {
		t.Fatalf("read %s correlation edges: %v", repo, err)
	}
	targets := make([]string, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, fmt.Sprint(row["target"]))
	}
	sort.Strings(targets)
	return targets
}

// TestLiveDeployableUnitRetractScopeIsIntentOrderIndependent runs the matching
// and the foreign-keyed intent in both orders and racing (10 trials), each on
// a fresh repository, and requires one final graph: only the admitted edge.
// A generation that drops the deployment evidence, reached by the matching
// intent, retracts the edge; a foreign-keyed intent there changes nothing.
func TestLiveDeployableUnitRetractScopeIsIntentOrderIndependent(t *testing.T) {
	live := openRepoRetryLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	foreign := "repo:" + live.repoID("du-foreign")
	legs := map[string]func(repo string) error{
		"matching then foreign": func(repo string) error {
			if err := live.handleDUIntent(ctx, repo, "gen-1", true, "repo:"+repo); err != nil {
				return err
			}
			return live.handleDUIntent(ctx, repo, "gen-1", true, foreign)
		},
		"foreign then matching": func(repo string) error {
			if err := live.handleDUIntent(ctx, repo, "gen-1", true, foreign); err != nil {
				return err
			}
			return live.handleDUIntent(ctx, repo, "gen-1", true, "repo:"+repo)
		},
	}
	for trial := 0; trial < 10; trial++ {
		legs[fmt.Sprintf("race %02d", trial)] = func(repo string) error {
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i, key := range []string{"repo:" + repo, foreign} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[i] = live.handleDUIntent(ctx, repo, "gen-1", true, key)
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
		repo := fmt.Sprintf("du-scope-%02d", index)
		index++
		live.seedDURepos(ctx, t, repo)
		if err := leg(repo); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := []string{live.repoID(repo + "-deploy")}
		if got := live.duEdgeTargets(ctx, t, repo); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: final correlation targets %v, want %v", name, got, want)
		}
		if name != "matching then foreign" {
			continue
		}
		// Generation 2 drops the Dockerfile. The foreign-keyed intent changes
		// nothing; the matching intent retracts the now-unsupported edge.
		if err := live.handleDUIntent(ctx, repo, "gen-2", false, foreign); err != nil {
			t.Fatalf("disappearance foreign: %v", err)
		}
		if got := live.duEdgeTargets(ctx, t, repo); !reflect.DeepEqual(got, want) {
			t.Fatalf("foreign-keyed gen-2 intent changed the graph to %v, want %v", got, want)
		}
		if err := live.handleDUIntent(ctx, repo, "gen-2", false, "repo:"+repo); err != nil {
			t.Fatalf("disappearance matching: %v", err)
		}
		if got := live.duEdgeTargets(ctx, t, repo); len(got) != 0 {
			t.Fatalf("matching gen-2 intent left %v, want every correlation edge retracted", got)
		}
	}
}
