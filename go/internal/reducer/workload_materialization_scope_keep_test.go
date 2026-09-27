// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/retract"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// The #7285 retract keep-list is a fact of the scope generation, not of the
// intent (arbiter option B). CorrelatedWorkloadProjectionInputLoader narrows
// its candidates to the intent's entity keys, while the retract covers every
// full-generation repository in the scope. A workload_materialization intent
// keyed to a repository outside its scope (a repo_dependency or
// deployment_mapping replay) must therefore still keep every workload and
// endpoint the scope generation admits. These tests drive the real loader
// from repository and file facts; every earlier retract test injected a
// loader that ignored the intent and so could not see the filter.

const (
	scopeKeepScopeID     = "scope-7285-orders"
	scopeKeepRepoID      = "repo-7285-orders"
	scopeKeepRepoName    = "orders-api"
	scopeKeepForeignRepo = "repo-7285-billing"
	scopeKeepDefinesDel  = "(repo:Repository {id: row.repo_id})-[rel:DEFINES]"
	scopeKeepEndpointDel = "(repo:Repository {id: row.repo_id})-[rel:EXPOSES_ENDPOINT]"
	scopeKeepDefinesPut  = "MERGE (repo)-[rel:DEFINES]->(w)"
)

// scopeKeepIDs returns the workload and endpoint ids the scope's one admitted
// candidate projects to, from the projection builder's own id helpers.
func scopeKeepIDs() (string, string) {
	workloadID := workloadid.NewWorkloadID(scopeKeepRepoID, scopeKeepRepoName).String()
	return workloadID, stableAPIEndpointID(scopeKeepRepoID, workloadID, "/orders")
}

// scopeKeepFacts is one full-generation repository. With a workload it also
// carries an ArgoCD Application declaring a Deployment (admitted without any
// resolved relationship) and an OpenAPI spec exposing /orders.
func scopeKeepFacts(withWorkload bool) []facts.Envelope {
	now := time.Now().UTC()
	envelopes := []facts.Envelope{{
		FactID: "fact-repo", ScopeID: scopeKeepScopeID, FactKind: factKindRepository, ObservedAt: now,
		Payload: map[string]any{"graph_id": scopeKeepRepoID, "name": scopeKeepRepoName},
	}}
	if !withWorkload {
		return envelopes
	}
	return append(envelopes,
		facts.Envelope{
			FactID: "fact-application", ScopeID: scopeKeepScopeID, FactKind: factKindFile, ObservedAt: now,
			Payload: map[string]any{
				"repo_id": scopeKeepRepoID, "relative_path": "deploy/application.yaml",
				"artifact_type": "argocd", "language": "yaml",
				"parsed_file_data": map[string]any{"k8s_resources": []any{
					map[string]any{"kind": "Deployment", "namespace": "prod"},
				}},
			},
		},
		facts.Envelope{
			FactID: "fact-openapi", ScopeID: scopeKeepScopeID, FactKind: factKindFile, ObservedAt: now,
			Payload: map[string]any{
				"repo_id": scopeKeepRepoID, "relative_path": "specs/openapi.yaml",
				"parsed_file_data": map[string]any{"source": "openapi: 3.1.0\ninfo:\n  version: v1\n" +
					"paths:\n  /orders:\n    get:\n      operationId: listOrders\n"},
			},
		},
	)
}

func scopeKeepIntent(keys []string) Intent {
	now := time.Now().UTC()
	return Intent{
		IntentID: "intent-scope-keep", ScopeID: scopeKeepScopeID, GenerationID: "gen-2",
		SourceSystem: "git", Domain: DomainWorkloadMaterialization, Cause: "facts projected",
		EntityKeys: keys, RelatedScopeIDs: []string{scopeKeepScopeID},
		EnqueuedAt: now, AvailableAt: now, Status: IntentStatusPending,
	}
}

// scopeKeepKeyCases are the entity-key shapes a workload_materialization
// intent reaches a scope with. match reports whether the loader's entity-key
// filter selects the scope's repository, i.e. whether the intent writes it.
var scopeKeepKeyCases = []struct {
	name  string
	keys  []string
	match bool
}{
	{name: "matching workload key", keys: []string{"workload:" + scopeKeepRepoName}, match: true},
	{name: "matching repo key", keys: []string{"repo:" + scopeKeepRepoID}, match: true},
	{name: "foreign repo key", keys: []string{"repo:" + scopeKeepForeignRepo}, match: false},
	{name: "scope-id fallback key", keys: []string{"repo:" + scopeKeepScopeID}, match: false},
	{name: "no keys", keys: nil, match: true},
}

// scopeKeepEdgeReader serves the retract guard's reads from a fixed set of
// current DEFINES and repository-side EXPOSES_ENDPOINT targets per repository.
type scopeKeepEdgeReader struct {
	mu        sync.Mutex
	defines   map[string][]string
	endpoints map[string][]string
	reads     int
}

func (r *scopeKeepEdgeReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	targets := r.endpoints
	if strings.Contains(cypher, ":DEFINES]") {
		targets = r.defines
	}
	repoIDs, _ := params["repo_ids"].([]string)
	var rows []map[string]any
	for _, repoID := range repoIDs {
		for _, target := range targets[repoID] {
			rows = append(rows, map[string]any{"repo_id": repoID, "target_id": target})
		}
	}
	return rows, nil
}

func (r *scopeKeepEdgeReader) readCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads
}

// scopeKeepCurrentEdges is a reader whose graph holds the scope repository's
// current DEFINES and EXPOSES_ENDPOINT edges, as a sibling intent wrote them.
func scopeKeepCurrentEdges() *scopeKeepEdgeReader {
	workloadID, endpointID := scopeKeepIDs()
	return &scopeKeepEdgeReader{
		defines:   map[string][]string{scopeKeepRepoID: {workloadID}},
		endpoints: map[string][]string{scopeKeepRepoID: {endpointID}},
	}
}

// scopeKeepHandler wires the real correlated input loader over envelopes. A
// nil reader leaves the guard unwired (the unguarded keep-list path).
func scopeKeepHandler(envelopes []facts.Envelope, executor CypherExecutor, reader *scopeKeepEdgeReader) WorkloadMaterializationHandler {
	factLoader := &stubFactLoader{envelopes: envelopes}
	handler := WorkloadMaterializationHandler{
		FactLoader:   factLoader,
		InputLoader:  CorrelatedWorkloadProjectionInputLoader{FactLoader: factLoader},
		Materializer: NewWorkloadMaterializer(executor),
	}
	if reader != nil {
		handler.RepositoryEdgeReader = retract.Reader(reader)
	}
	return handler
}

// scopeKeepStatements returns the recorded statements containing every shape.
func scopeKeepStatements(calls []recordedCypherCall, shapes ...string) []recordedCypherCall {
	var matched []recordedCypherCall
	for _, call := range calls {
		all := true
		for _, shape := range shapes {
			all = all && strings.Contains(call.cypher, shape)
		}
		if all {
			matched = append(matched, call)
		}
	}
	return matched
}

func scopeKeepRetractDeletes(calls []recordedCypherCall) []recordedCypherCall {
	return append(scopeKeepStatements(calls, scopeKeepDefinesDel, "DELETE rel"),
		scopeKeepStatements(calls, scopeKeepEndpointDel, "DELETE rel")...)
}

// TestWorkloadMaterializationKeepListIsScopeWideForEveryEntityKey is proof 1
// of the #7285 retract-scope ruling. Whatever the intent's keys, the keep-list
// for the scope's repository holds the workload and endpoint the scope
// generation admits, so the guarded retract deletes nothing current, while
// the write set stays filtered to the keys.
func TestWorkloadMaterializationKeepListIsScopeWideForEveryEntityKey(t *testing.T) {
	t.Parallel()

	workloadID, endpointID := scopeKeepIDs()
	for _, tc := range scopeKeepKeyCases {
		t.Run(tc.name+"/guarded", func(t *testing.T) {
			t.Parallel()
			executor := &recordingCypherExecutor{}
			reader := scopeKeepCurrentEdges()
			handler := scopeKeepHandler(scopeKeepFacts(true), executor, reader)
			if _, err := handler.Handle(context.Background(), scopeKeepIntent(tc.keys)); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if reader.readCount() == 0 {
				t.Fatal("retract guard never read the graph: the retract was skipped")
			}
			if deletes := scopeKeepRetractDeletes(executor.calls); len(deletes) > 0 {
				t.Fatalf("keys %v deleted a current repository edge (%d DELETE statement(s), first %v); "+
					"the keep-list must be the scope generation's admitted set, not the entity-filtered one",
					tc.keys, len(deletes), deletes[0].params)
			}
			writes := scopeKeepStatements(executor.calls, scopeKeepDefinesPut)
			if tc.match != (len(writes) > 0) {
				t.Fatalf("keys %v wrote %d DEFINES statements, want writes=%v: the write set must stay entity-filtered",
					tc.keys, len(writes), tc.match)
			}
		})
		t.Run(tc.name+"/unguarded", func(t *testing.T) {
			t.Parallel()
			executor := &recordingCypherExecutor{}
			handler := scopeKeepHandler(scopeKeepFacts(true), executor, nil)
			if _, err := handler.Handle(context.Background(), scopeKeepIntent(tc.keys)); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			want := []map[string]any{{
				"repo_id":           scopeKeepRepoID,
				"keep_workload_ids": []string{workloadID},
				"keep_endpoint_ids": []string{endpointID},
			}}
			for _, shape := range []string{scopeKeepDefinesDel, scopeKeepEndpointDel} {
				deletes := scopeKeepStatements(executor.calls, shape, "DELETE rel")
				if len(deletes) != 1 {
					t.Fatalf("keys %v sent %d %s keep-list statements, want 1", tc.keys, len(deletes), shape)
				}
				if got := deletes[0].params["rows"]; !reflect.DeepEqual(got, want) {
					t.Fatalf("keys %v %s keep-list rows = %#v, want %#v", tc.keys, shape, got, want)
				}
			}
		})
	}
}

// TestWorkloadMaterializationForeignKeyedIntentRetractsTrueDisappearance is
// proof 3. Generation 2 drops the workload and the only intent that runs is
// keyed to a foreign repository. The retract must still delete the stale
// DEFINES and EXPOSES_ENDPOINT edges: a keep-list or repository set bounded
// by the intent's keys would never fire here.
func TestWorkloadMaterializationForeignKeyedIntentRetractsTrueDisappearance(t *testing.T) {
	t.Parallel()

	workloadID, endpointID := scopeKeepIDs()
	executor := &recordingCypherExecutor{}
	handler := scopeKeepHandler(scopeKeepFacts(false), executor, scopeKeepCurrentEdges())
	if _, err := handler.Handle(context.Background(), scopeKeepIntent([]string{"repo:" + scopeKeepForeignRepo})); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	for shape, target := range map[string]string{scopeKeepDefinesDel: workloadID, scopeKeepEndpointDel: endpointID} {
		deletes := scopeKeepStatements(executor.calls, shape, "DELETE rel")
		if len(deletes) != 1 {
			t.Fatalf("%s: %d DELETE statements, want 1 for the disappeared target %s", shape, len(deletes), target)
		}
		want := []map[string]any{{"repo_id": scopeKeepRepoID, "target_id": target}}
		if got := deletes[0].params["rows"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s delete rows = %#v, want %#v", shape, got, want)
		}
	}
}

// failingDefinesUpsertExecutor fails the DEFINES upsert, so Materialize
// returns an error before the retract.
type failingDefinesUpsertExecutor struct {
	recordingCypherExecutor
}

func (f *failingDefinesUpsertExecutor) ExecuteCypher(ctx context.Context, cypher string, params map[string]any) error {
	if strings.Contains(cypher, scopeKeepDefinesPut) {
		return errors.New("graph write failed")
	}
	return f.recordingCypherExecutor.ExecuteCypher(ctx, cypher, params)
}

func (f *failingDefinesUpsertExecutor) ExecuteCypherGroup(ctx context.Context, statements []CypherGroupStatement) error {
	for _, statement := range statements {
		if err := f.ExecuteCypher(ctx, statement.Cypher, statement.Parameters); err != nil {
			return err
		}
	}
	return nil
}

// TestWorkloadMaterializationDeferralAndFailedWriteIssueNoRetract is proof 5.
// A resolution_not_ready deferral returns before any graph statement, and a
// failed Materialize returns before the retract, so neither can delete an
// edge a sibling intent wrote.
func TestWorkloadMaterializationDeferralAndFailedWriteIssueNoRetract(t *testing.T) {
	t.Parallel()

	t.Run("resolution not ready", func(t *testing.T) {
		t.Parallel()
		executor := &recordingCypherExecutor{}
		reader := scopeKeepCurrentEdges()
		factLoader := &stubFactLoader{envelopes: scopeKeepFacts(true)}
		handler := WorkloadMaterializationHandler{
			FactLoader: factLoader,
			InputLoader: CorrelatedWorkloadProjectionInputLoader{
				FactLoader:             factLoader,
				ResolutionActiveLookup: func(string) (bool, error) { return false, nil },
			},
			Materializer:         NewWorkloadMaterializer(executor),
			RepositoryEdgeReader: reader,
		}
		_, err := handler.Handle(context.Background(), scopeKeepIntent([]string{"workload:" + scopeKeepRepoName}))
		var classified interface{ FailureClass() string }
		if !errors.As(err, &classified) || classified.FailureClass() != WorkloadMaterializationResolutionNotReadyFailureClass {
			t.Fatalf("Handle() error = %v, want the resolution-not-ready deferral", err)
		}
		if len(executor.calls) != 0 || reader.readCount() != 0 {
			t.Fatalf("deferral issued %d graph statements and %d guard reads, want 0 and 0", len(executor.calls), reader.readCount())
		}
	})
	t.Run("failed materialize", func(t *testing.T) {
		t.Parallel()
		executor := &failingDefinesUpsertExecutor{}
		reader := scopeKeepCurrentEdges()
		handler := scopeKeepHandler(scopeKeepFacts(true), executor, reader)
		_, err := handler.Handle(context.Background(), scopeKeepIntent([]string{"workload:" + scopeKeepRepoName}))
		if err == nil || !strings.Contains(err.Error(), "graph write failed") {
			t.Fatalf("Handle() error = %v, want the failed DEFINES write", err)
		}
		if deletes := scopeKeepRetractDeletes(executor.calls); len(deletes) != 0 || reader.readCount() != 0 {
			t.Fatalf("failed Materialize issued %d retract statements and %d guard reads, want 0 and 0",
				len(deletes), reader.readCount())
		}
	})
}

// TestProjectionWorkloadAndEndpointIDsIgnoreEnvironmentsAndPlatforms pins why
// the scope keep-list may be built without the write path's infrastructure
// platform read: the projection builder derives workload and endpoint ids
// from the candidate alone.
func TestProjectionWorkloadAndEndpointIDsIgnoreEnvironmentsAndPlatforms(t *testing.T) {
	t.Parallel()

	candidate := repoEdgeRetractCandidate(scopeKeepRepoID, scopeKeepRepoName)
	candidate.Namespaces = []string{"prod"}
	bare := BuildProjectionRowsWithInfrastructurePlatforms([]WorkloadCandidate{candidate}, nil, nil)
	full := BuildProjectionRowsWithInfrastructurePlatforms(
		[]WorkloadCandidate{candidate},
		map[string][]string{scopeKeepRepoID: {"prod", "staging"}},
		map[string][]InfrastructurePlatformRow{scopeKeepRepoID: {{PlatformID: "platform:eks:prod", PlatformKind: "eks", PlatformName: "prod"}}},
	)
	ids := func(p *ProjectionResult) string {
		var workloads, endpoints []string
		for _, row := range p.WorkloadRows {
			workloads = append(workloads, row.RepoID+"|"+row.WorkloadID)
		}
		for _, row := range p.EndpointRows {
			endpoints = append(endpoints, row.RepoID+"|"+row.EndpointID)
		}
		slices.Sort(workloads)
		slices.Sort(endpoints)
		return fmt.Sprint(workloads, endpoints)
	}
	if ids(bare) != ids(full) || len(bare.WorkloadRows) == 0 || len(bare.EndpointRows) == 0 {
		t.Fatalf("projection ids depend on environments or platforms: bare %s, full %s", ids(bare), ids(full))
	}
}

// intentOnlyInputLoader returns only the entity-filtered candidates: it has
// no scope truth to build a keep-list from.
type intentOnlyInputLoader struct{ candidates []WorkloadCandidate }

func (l intentOnlyInputLoader) LoadWorkloadProjectionInputs(context.Context, Intent) ([]WorkloadCandidate, map[string][]string, error) {
	return l.candidates, nil, nil
}

// TestWorkloadMaterializationSkipsRetractWithoutScopeTruth pins the fallback
// rule: a loader that cannot supply the scope generation's admitted set skips
// the retract (and warns) rather than keep its entity-filtered candidates,
// on both the write path and the zero-candidate path.
func TestWorkloadMaterializationSkipsRetractWithoutScopeTruth(t *testing.T) {
	t.Parallel()

	for name, candidates := range map[string][]WorkloadCandidate{
		"with candidates": {repoEdgeRetractCandidate(scopeKeepRepoID, scopeKeepRepoName)},
		"zero candidates": nil,
	} {
		executor := &recordingCypherExecutor{}
		reader := scopeKeepCurrentEdges()
		handler := WorkloadMaterializationHandler{
			FactLoader:           &stubFactLoader{envelopes: scopeKeepFacts(true)},
			InputLoader:          intentOnlyInputLoader{candidates: candidates},
			Materializer:         NewWorkloadMaterializer(executor),
			RepositoryEdgeReader: reader,
		}
		if _, err := handler.Handle(context.Background(), scopeKeepIntent([]string{"repo:" + scopeKeepForeignRepo})); err != nil {
			t.Fatalf("%s: Handle() error = %v", name, err)
		}
		if deletes := scopeKeepRetractDeletes(executor.calls); len(deletes) != 0 || reader.readCount() != 0 {
			t.Fatalf("%s: loader without scope truth ran the retract (%d deletes, %d guard reads), want it skipped",
				name, len(deletes), reader.readCount())
		}
	}
}
