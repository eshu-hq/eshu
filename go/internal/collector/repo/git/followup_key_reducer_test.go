// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/platformfam"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// These tests close the #7316 loop end to end. The collector emits the real
// follow-up fact (streamFacts), the projector turns it into a reducer intent
// (BuildReducerIntent), and the real reducer handlers select the repository by
// that key. No key is hand-written: a hand-written key would prove the reducer
// against the author's guess, not against what the collector emits. They live
// beside the collector because projector/runtime imports the reducer package,
// so a reducer-package test cannot import the projector without a cycle.

const (
	displayNamedRepoName = "pkg-display"
	displayNamedRepoDir  = "checkout-dir"
	displayNamedRepoID   = "repository:r_7316abcd"
	displayNamedDeployID = "repository:r_7316deploy"
	displayNamedStaleID  = "repository:r_7316retired"
)

// displayNamedGeneration streams one full generation for a repository whose
// fact name (pkg-display) differs from its checkout basename (checkout-dir),
// the dependency-mode shape ESHU_BOOTSTRAP_PACKAGE_NAME produces. It returns
// the real repository fact and the real follow-up envelope per reducer_domain.
func displayNamedGeneration(t *testing.T) (facts.Envelope, map[string]facts.Envelope) {
	t.Helper()

	repoPath := filepath.Join(t.TempDir(), displayNamedRepoDir)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repoPath, err)
	}
	repo := repositoryidentity.Metadata{ID: displayNamedRepoID, Name: displayNamedRepoName, LocalPath: repoPath}
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-7316")

	collected := buildStreamingGeneration(repoPath, repo, "run-7316", time.Now().UTC(), snapshot, false, "")
	var repository facts.Envelope
	followups := map[string]facts.Envelope{}
	for _, envelope := range drainFactChannel(collected.Facts) {
		switch envelope.FactKind {
		case "repository":
			repository = envelope
		case "shared_followup":
			domain, _ := envelope.Payload["reducer_domain"].(string)
			followups[domain] = envelope
		}
	}
	if repository.FactKind == "" {
		t.Fatal("stream carried no repository fact")
	}
	return repository, followups
}

// reducerIntentFromFollowup runs the real projector conversion and returns the
// intent shape the reducer queue hands a handler (one entity key per intent).
func reducerIntentFromFollowup(t *testing.T, envelope facts.Envelope) reducer.Intent {
	t.Helper()

	built, ok := projectorruntime.BuildReducerIntent(envelope)
	if !ok {
		t.Fatalf("BuildReducerIntent(%v) ok = false", envelope.Payload["reducer_domain"])
	}
	return reducer.Intent{
		IntentID:     "intent-" + string(built.Domain),
		ScopeID:      built.ScopeID,
		GenerationID: built.GenerationID,
		SourceSystem: "git",
		Domain:       built.Domain,
		Cause:        built.Reason,
		EntityKeys:   []string{built.EntityKey},
	}
}

// displayNamedScopeFacts is the scope generation the reducer loads: the real
// repository fact plus a Dockerfile file fact that makes the repository a
// deployable-unit candidate (the same fixture the reducer's own tests use).
func displayNamedScopeFacts(repository facts.Envelope, deployable bool) []facts.Envelope {
	envelopes := []facts.Envelope{repository}
	if deployable {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   "fact-file-7316",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          displayNamedRepoID,
				"language":         "dockerfile",
				"relative_path":    "Dockerfile",
				"parsed_file_data": map[string]any{"dockerfile_stages": []any{map[string]any{"name": "runtime"}}},
			},
			ObservedAt: time.Now().UTC(),
		})
	}
	return envelopes
}

type scopeFactLoader struct{ envelopes []facts.Envelope }

func (l scopeFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

type deploysFromLoader struct{}

func (deploysFromLoader) GetResolvedRelationships(context.Context, string) ([]relationships.ResolvedRelationship, error) {
	return []relationships.ResolvedRelationship{{
		SourceRepoID: displayNamedDeployID, TargetRepoID: displayNamedRepoID,
		RelationshipType: relationships.RelDeploysFrom, Confidence: 0.94,
		Details: map[string]any{"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDAppSource)}},
	}}, nil
}

// edgeGraph mirrors the production CORRELATES_DEPLOYABLE_UNIT statements'
// semantics: retract deletes every edge whose source a row names, write MERGEs
// one edge per (repo_id, deployment_repo_id).
type edgeGraph struct {
	edges      map[string]struct{}
	statements int
}

func newEdgeGraph(seed ...string) *edgeGraph {
	g := &edgeGraph{edges: map[string]struct{}{}}
	for _, edge := range seed {
		g.edges[edge] = struct{}{}
	}
	return g
}

func (g *edgeGraph) RetractEdges(_ context.Context, _ string, rows []reducer.SharedProjectionIntentRow, _ string) error {
	g.statements++
	for _, row := range rows {
		for edge := range g.edges {
			if strings.HasPrefix(edge, row.RepositoryID+"->") {
				delete(g.edges, edge)
			}
		}
	}
	return nil
}

func (g *edgeGraph) WriteEdges(_ context.Context, _ string, rows []reducer.SharedProjectionIntentRow, _ string) (reducer.SharedProjectionWriteReport, error) {
	g.statements++
	for _, row := range rows {
		repoID, _ := row.Payload["repo_id"].(string)
		deployID, _ := row.Payload["deployment_repo_id"].(string)
		g.edges[repoID+"->"+deployID] = struct{}{}
	}
	return reducer.SharedProjectionWriteReport{}, nil
}

func (g *edgeGraph) sorted() []string {
	edges := make([]string, 0, len(g.edges))
	for edge := range g.edges {
		edges = append(edges, edge)
	}
	sort.Strings(edges)
	return edges
}

func runDeployableUnit(t *testing.T, graph *edgeGraph, repository facts.Envelope, deployable bool, intent reducer.Intent) {
	t.Helper()

	handler := reducer.DeployableUnitCorrelationHandler{
		FactLoader:     scopeFactLoader{envelopes: displayNamedScopeFacts(repository, deployable)},
		ResolvedLoader: deploysFromLoader{},
		EdgeWriter:     graph,
	}
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("DeployableUnitCorrelationHandler.Handle() error = %v", err)
	}
}

// TestDeployableUnitCorrelationSelectsDisplayNamedRepository is the #7316
// regression for deployable_unit_correlation. With the collector's real key the
// handler writes the repository's edge and retracts a stale one; when the
// deployment evidence is gone it retracts them all; an intent keyed to another
// repository still leaves it alone (the #7304 invariant); and the pre-fix key
// (checkout basename) is a no-op, which is exactly the defect the fix removes.
func TestDeployableUnitCorrelationSelectsDisplayNamedRepository(t *testing.T) {
	t.Parallel()

	repository, followups := displayNamedGeneration(t)
	intent := reducerIntentFromFollowup(t, followups["deployable_unit_correlation"])
	if got, want := intent.EntityKeys, []string{"repo:" + displayNamedRepoID}; !reflect.DeepEqual(got, want) {
		t.Errorf("collector deployable_unit_correlation keys = %v, want %v", got, want)
	}
	seed := []string{displayNamedRepoID + "->" + displayNamedDeployID, displayNamedRepoID + "->" + displayNamedStaleID}

	t.Run("writes the edge and retracts the stale one", func(t *testing.T) {
		t.Parallel()
		graph := newEdgeGraph(seed...)
		runDeployableUnit(t, graph, repository, true, intent)
		if got, want := graph.sorted(), []string{displayNamedRepoID + "->" + displayNamedDeployID}; !reflect.DeepEqual(got, want) {
			t.Fatalf("edges = %v, want %v", got, want)
		}
	})
	t.Run("retracts every edge when the deployment evidence disappeared", func(t *testing.T) {
		t.Parallel()
		graph := newEdgeGraph(seed...)
		runDeployableUnit(t, graph, repository, false, intent)
		if got := graph.sorted(); len(got) != 0 {
			t.Fatalf("edges = %v, want none", got)
		}
	})
	t.Run("an intent keyed to another repository leaves it untouched", func(t *testing.T) {
		t.Parallel()
		graph := newEdgeGraph(seed...)
		foreign := intent
		foreign.EntityKeys = []string{"repo:repository:r_other"}
		runDeployableUnit(t, graph, repository, true, foreign)
		if graph.statements != 0 || !reflect.DeepEqual(graph.sorted(), sortedCopy(seed)) {
			t.Fatalf("foreign intent statements = %d edges = %v, want 0 and untouched", graph.statements, graph.sorted())
		}
	})
	t.Run("a name key selects nothing after the id switch", func(t *testing.T) {
		t.Parallel()
		graph := newEdgeGraph(seed...)
		legacy := intent
		legacy.EntityKeys = []string{"repo:" + displayNamedRepoName}
		runDeployableUnit(t, graph, repository, true, legacy)
		if graph.statements != 0 || !reflect.DeepEqual(graph.sorted(), sortedCopy(seed)) {
			t.Fatalf("name-key intent statements = %d edges = %v, want 0 and untouched", graph.statements, graph.sorted())
		}
	})
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// TestWorkloadProjectionLoaderSelectsDisplayNamedRepository is the #7316
// regression for workload_materialization writes: the collector's real
// workload_materialization key must select the scope's admitted candidate, so
// Candidates (what the intent writes) equals ScopeCandidates (the whole admitted
// scope, which a single-repository git scope makes the same set).
func TestWorkloadProjectionLoaderSelectsDisplayNamedRepository(t *testing.T) {
	t.Parallel()

	repository, followups := displayNamedGeneration(t)
	loader := reducer.CorrelatedWorkloadProjectionInputLoader{
		FactLoader:     scopeFactLoader{envelopes: displayNamedScopeFacts(repository, true)},
		ResolvedLoader: deploysFromLoader{},
	}
	intent := reducerIntentFromFollowup(t, followups["workload_materialization"])
	if got, want := intent.EntityKeys, []string{"workload:" + displayNamedRepoID}; !reflect.DeepEqual(got, want) {
		t.Errorf("collector workload_materialization keys = %v, want %v", got, want)
	}

	inputs, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), intent)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionScopeInputs() error = %v", err)
	}
	if len(inputs.ScopeCandidates) != 1 {
		t.Fatalf("admitted scope candidates = %d, want 1 (fixture must admit the repository)", len(inputs.ScopeCandidates))
	}
	if !reflect.DeepEqual(inputs.Candidates, inputs.ScopeCandidates) {
		t.Fatalf("Candidates = %+v, want the admitted scope set %+v", inputs.Candidates, inputs.ScopeCandidates)
	}
	if got := inputs.Candidates[0].RepoName; got != displayNamedRepoName {
		t.Fatalf("candidate RepoName = %q, want %q", got, displayNamedRepoName)
	}

	legacy := intent
	legacy.EntityKeys = []string{"workload:" + displayNamedRepoName}
	legacyInputs, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), legacy)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionScopeInputs(legacy) error = %v", err)
	}
	if len(legacyInputs.Candidates) != 0 {
		t.Fatalf("name key selected %d candidates, want 0 (names no longer select)", len(legacyInputs.Candidates))
	}
}

type replayRecorder struct{ keys []string }

func (r *replayRecorder) ReplayWorkloadMaterialization(_ context.Context, _, _, entityKey string) (bool, error) {
	r.keys = append(r.keys, entityKey)
	return true, nil
}

type stubPlatformWriter struct{}

func (stubPlatformWriter) WritePlatformMaterialization(context.Context, platformfam.PlatformMaterializationWrite) (platformfam.PlatformMaterializationWriteResult, error) {
	return platformfam.PlatformMaterializationWriteResult{CanonicalWrites: 1}, nil
}

type crossRepoWrites struct{}

func (crossRepoWrites) Resolve(context.Context, string, string) (int, error) { return 1, nil }

// TestPlatformReplayKeySelectsDisplayNamedRepository covers the deployment_mapping
// path: the real deployment:<name> intent makes the platform handler replay
// workload materialization under a repo:<alias> key, and that replayed key must
// select the repository through the same candidate filter.
func TestPlatformReplayKeySelectsDisplayNamedRepository(t *testing.T) {
	t.Parallel()

	repository, followups := displayNamedGeneration(t)
	intent := reducerIntentFromFollowup(t, followups["deployment_mapping"])
	if got, want := intent.EntityKeys, []string{"deployment:" + displayNamedRepoID}; !reflect.DeepEqual(got, want) {
		t.Errorf("collector deployment_mapping keys = %v, want %v", got, want)
	}
	intent.RelatedScopeIDs = []string{intent.ScopeID}

	replayer := &replayRecorder{}
	handler := platformfam.PlatformMaterializationHandler{
		Writer:                          stubPlatformWriter{},
		CrossRepoResolver:               crossRepoWrites{},
		WorkloadMaterializationReplayer: replayer,
	}
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("PlatformMaterializationHandler.Handle() error = %v", err)
	}
	if got, want := replayer.keys, []string{"repo:r_7316abcd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replay entity keys = %v, want %v", got, want)
	}

	loader := reducer.CorrelatedWorkloadProjectionInputLoader{
		FactLoader:     scopeFactLoader{envelopes: displayNamedScopeFacts(repository, true)},
		ResolvedLoader: deploysFromLoader{},
	}
	replayed := intent
	replayed.Domain = reducer.DomainWorkloadMaterialization
	replayed.EntityKeys = []string{replayer.keys[0]}
	inputs, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), replayed)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionScopeInputs(replayed) error = %v", err)
	}
	if len(inputs.Candidates) != 1 {
		t.Fatalf("replayed key selected %d candidates, want 1", len(inputs.Candidates))
	}
}
