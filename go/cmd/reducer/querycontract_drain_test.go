// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/semanticsearch"
	"github.com/eshu-hq/eshu/go/internal/reducer/workloadinstance"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
)

// fakeDrainReader is a scripted querycontract.GraphQuery proving the drained
// reducer seams decode graph rows exactly as they did through package query.
// query.GraphQuery is an alias of querycontract.GraphQuery, so a fake built
// against the contract also satisfies every pre-drain signature.
type fakeDrainReader struct {
	rows       []map[string]any
	err        error
	lastCypher string
	lastParams map[string]any
}

var _ querycontract.GraphQuery = (*fakeDrainReader)(nil)

func (f *fakeDrainReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.lastCypher = cypher
	f.lastParams = params
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeDrainReader) RunSingle(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
	f.lastCypher = cypher
	f.lastParams = params
	if f.err != nil {
		return nil, f.err
	}
	if len(f.rows) == 0 {
		return nil, nil
	}
	return f.rows[0], nil
}

func drainEnv(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// TestDrainECSBindingLookupDecodesRows covers ecs_binding_lookups.go: the
// querycontract-backed reader trims environments, drops blank legs, and
// sorts the per-workload environment lists.
func TestDrainECSBindingLookupDecodesRows(t *testing.T) {
	reader := &fakeDrainReader{rows: []map[string]any{
		{"name": "web", "environment": " prod "},
		{"name": "web", "environment": "prod"},
		{"name": "web", "environment": "staging"},
		{"name": "", "environment": "prod"},
		{"name": "api", "environment": ""},
	}}
	got, err := neo4jECSBindingInstanceLookup{reader: reader}.ListECSBindingInstanceEnvironments(
		context.Background(), []string{"web", "api"})
	if err != nil {
		t.Fatalf("ListECSBindingInstanceEnvironments() error = %v", err)
	}
	want := map[string][]string{"web": {"prod", "staging"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListECSBindingInstanceEnvironments() = %v, want %v", got, want)
	}
	nilLookup := neo4jECSBindingInstanceLookup{}
	got, err = nilLookup.ListECSBindingInstanceEnvironments(context.Background(), []string{"web"})
	if err != nil || len(got) != 0 {
		t.Fatalf("nil reader = %v, %v; want empty map, nil", got, err)
	}
}

// TestDrainECSBindingLookupPropagatesErrors covers the error leg of the same
// seam: a failing graph read surfaces instead of an empty map.
func TestDrainECSBindingLookupPropagatesErrors(t *testing.T) {
	boom := errors.New("graph down")
	lookup := neo4jECSBindingInstanceLookup{reader: &fakeDrainReader{err: boom}}
	if _, err := lookup.ListECSBindingInstanceEnvironments(
		context.Background(), []string{"web"}); !errors.Is(err, boom) {
		t.Fatalf("ListECSBindingInstanceEnvironments() error = %v, want %v", err, boom)
	}
}

// TestDrainWorkloadDependencyLookupDecodesRows covers
// workload_dependency_lookup.go: blank repo ids are skipped on every leg.
func TestDrainWorkloadDependencyLookupDecodesRows(t *testing.T) {
	ctx := context.Background()
	reader := &fakeDrainReader{rows: []map[string]any{
		{"source_repo_id": "s1", "target_repo_id": "t1"},
		{"source_repo_id": "", "target_repo_id": "t2"},
	}}
	edges, err := neo4jWorkloadDependencyLookup{reader: reader}.ListRepoDependencyEdges(ctx, []string{"s1"})
	if err != nil || len(edges) != 1 || edges[0].SourceRepoID != "s1" || edges[0].TargetRepoID != "t1" {
		t.Fatalf("ListRepoDependencyEdges() = %+v, %v; want one s1->t1 edge", edges, err)
	}
	workloads, err := neo4jWorkloadDependencyLookup{reader: &fakeDrainReader{rows: []map[string]any{
		{"repo_id": "r1", "workload_id": "w1"},
		{"repo_id": "r1", "workload_id": ""},
	}}}.ListRepoWorkloads(ctx, []string{"r1"})
	if err != nil || len(workloads) != 1 || workloads[0].WorkloadID != "w1" {
		t.Fatalf("ListRepoWorkloads() = %+v, %v; want one r1/w1 row", workloads, err)
	}
	retract, err := neo4jWorkloadDependencyLookup{reader: &fakeDrainReader{rows: []map[string]any{
		{"repo_id": "r9"},
		{"repo_id": ""},
	}}}.ListWorkloadDependencyEdges(ctx, []string{"r9"}, "evidence")
	if err != nil || len(retract) != 1 || retract[0].RepoID != "r9" {
		t.Fatalf("ListWorkloadDependencyEdges() = %+v, %v; want one r9 row", retract, err)
	}
	nilDep := neo4jWorkloadDependencyLookup{}
	if _, err := nilDep.ListRepoDependencyEdges(ctx, []string{"s1"}); err != nil {
		t.Fatalf("nil reader ListRepoDependencyEdges() error = %v, want nil", err)
	}
}

// TestDrainWorkloadInstanceRetractionLookupDecodesRows covers
// workload_instance_retraction_lookup.go: rows decode repo/instance pairs and
// incomplete rows are dropped.
func TestDrainWorkloadInstanceRetractionLookupDecodesRows(t *testing.T) {
	reader := &fakeDrainReader{rows: []map[string]any{
		{"repo_id": "r1", "instance_id": "i1"},
		{"repo_id": "r1", "instance_id": ""},
	}}
	got, err := neo4jWorkloadInstanceRetractionLookup{reader: reader}.ListWorkloadInstances(
		context.Background(), []string{"r1"}, "evidence")
	if err != nil || len(got) != 1 || got[0].RepoID != "r1" || got[0].InstanceID != "i1" {
		t.Fatalf("ListWorkloadInstances() = %+v, %v; want one r1/i1 row", got, err)
	}
	nilRetract := neo4jWorkloadInstanceRetractionLookup{}
	if _, err := nilRetract.ListWorkloadInstances(context.Background(), []string{"r1"}, "evidence"); err != nil {
		t.Fatalf("nil reader ListWorkloadInstances() error = %v, want nil", err)
	}
}

// TestDrainWorkloadSiblingLookupDecodesRows covers
// workload_sibling_lookup.go: siblings with any blank field are skipped.
func TestDrainWorkloadSiblingLookupDecodesRows(t *testing.T) {
	reader := &fakeDrainReader{rows: []map[string]any{
		{"id": "w1", "name": "web", "repo_id": "r1"},
		{"id": "w2", "name": "", "repo_id": "r2"},
	}}
	got, err := neo4jWorkloadSiblingLookup{reader: reader}.ListWorkloadSiblings(
		context.Background(), []string{"web"})
	if err != nil || len(got) != 1 || got[0].ID != "w1" || got[0].RepoID != "r1" {
		t.Fatalf("ListWorkloadSiblings() = %+v, %v; want one w1 row", got, err)
	}
	nilSibling := neo4jWorkloadSiblingLookup{}
	if _, err := nilSibling.ListWorkloadSiblings(context.Background(), []string{"web"}); err != nil {
		t.Fatalf("nil reader ListWorkloadSiblings() error = %v, want nil", err)
	}
}

// TestDrainProjectorDrainGateProfiles covers config.go: the projector drain
// gate still resolves through the querycontract profile vocabulary.
func TestDrainProjectorDrainGateProfiles(t *testing.T) {
	for name, tc := range map[string]struct {
		profile string
		backend runtimecfg.GraphBackend
		want    bool
		wantErr bool
	}{
		"nornicdb authoritative drains": {profile: "local_authoritative", backend: runtimecfg.GraphBackendNornicDB, want: true},
		"nornicdb lightweight skips":    {profile: "local_lightweight", backend: runtimecfg.GraphBackendNornicDB},
		"neo4j authoritative skips":     {profile: "local_authoritative", backend: runtimecfg.GraphBackendNeo4j},
		"unknown profile fails":         {profile: "bogus", backend: runtimecfg.GraphBackendNornicDB, wantErr: true},
		"contract profile identical": {
			profile: string(querycontract.ProfileLocalAuthoritative),
			backend: runtimecfg.GraphBackendNornicDB,
			want:    true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := loadReducerProjectorDrainGate(
				drainEnv(map[string]string{queryProfileEnv: tc.profile}), tc.backend)
			if tc.wantErr != (err != nil) {
				t.Fatalf("loadReducerProjectorDrainGate(%q) error = %v, wantErr %v", tc.profile, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("loadReducerProjectorDrainGate(%q, %q) = %v, want %v", tc.profile, tc.backend, got, tc.want)
			}
		})
	}
}

// TestDrainValidateGenerationRetentionProfiles covers
// generation_retention_wiring.go: disabled retention still requires an
// explicit local profile, parsed from the querycontract vocabulary.
func TestDrainValidateGenerationRetentionProfiles(t *testing.T) {
	t.Run("enabled skips profile check", func(t *testing.T) {
		cfg := generationRetentionConfig{Enabled: true}
		if err := validateGenerationRetentionConfig(drainEnv(nil), cfg); err != nil {
			t.Fatalf("validateGenerationRetentionConfig() error = %v, want nil", err)
		}
	})
	for name, tc := range map[string]struct {
		profile string
		wantErr bool
	}{
		"authoritative passes": {profile: "local_authoritative"},
		"lightweight passes":   {profile: "local_lightweight"},
		"full stack passes":    {profile: "local_full_stack"},
		"unknown fails":        {profile: "remote", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateGenerationRetentionConfig(
				drainEnv(map[string]string{queryProfileEnv: tc.profile}), generationRetentionConfig{})
			if tc.wantErr != (err != nil) {
				t.Fatalf("validateGenerationRetentionConfig(%q) error = %v, wantErr %v", tc.profile, err, tc.wantErr)
			}
		})
	}
}

// TestDrainWorkloadInstanceExistenceWiring covers
// cross_scope_readiness_wiring.go: the wired lookup answers from the
// contract-typed reader.
func TestDrainWorkloadInstanceExistenceWiring(t *testing.T) {
	reader := &fakeDrainReader{rows: []map[string]any{
		{"ready_workload_id": "w1", "ready_environment": "prod"},
		{"ready_workload_id": "", "ready_environment": "prod"},
	}}
	lookup := workloadInstanceExistenceFor(reader)
	graphLookup, ok := lookup.(workloadinstance.GraphExistenceLookup)
	if !ok {
		t.Fatalf("workloadInstanceExistenceFor() type = %T, want workloadinstance.GraphExistenceLookup", lookup)
	}
	got, err := graphLookup.ExistingAnchors(context.Background(), []workloadinstance.Anchor{
		{WorkloadID: "w1", Environment: "prod"},
	})
	if err != nil {
		t.Fatalf("ExistingAnchors() error = %v", err)
	}
	if _, ok := got[workloadinstance.Anchor{WorkloadID: "w1", Environment: "prod"}]; !ok || len(got) != 1 {
		t.Fatalf("ExistingAnchors() = %v, want exactly the w1/prod anchor", got)
	}
}

// TestDrainApplyReducerCaptureNilSession covers neo4j_wiring.go: a nil
// capture session returns both seams unchanged, now typed through the
// contract port.
func TestDrainApplyReducerCaptureNilSession(t *testing.T) {
	exec := newReducerNeo4jExecutor(&fakeNeo4jSession{}, nil)
	reader := &fakeDrainReader{rows: []map[string]any{{"n": 1}}}
	gotExec, gotReader := applyReducerCapture(nil, exec, reader)
	if !reflect.DeepEqual(gotExec, exec) {
		t.Fatalf("applyReducerCapture(nil) executor changed: %T", gotExec)
	}
	if gotReader != querycontract.GraphQuery(reader) {
		t.Fatalf("applyReducerCapture(nil) reader changed: %T", gotReader)
	}
	row, err := gotReader.RunSingle(context.Background(), "RETURN 1 AS n", nil)
	if err != nil || row["n"] != 1 {
		t.Fatalf("decorated RunSingle() = %v, %v; want {n:1}, nil", row, err)
	}
}

// TestDrainIdAnchorCensusReaderFallback covers run.go: the census prefers the
// raw port, falls back to the contract-typed decorated reader, and reports
// nil when neither is configured.
func TestDrainIdAnchorCensusReaderFallback(t *testing.T) {
	reader := &fakeDrainReader{rows: []map[string]any{{"n": 1}}}
	if got := idAnchorCensusReader(nil, reader); got == nil {
		t.Fatal("idAnchorCensusReader(nil, reader) = nil, want the decorated reader")
	} else if row, err := got.RunSingle(context.Background(), "RETURN 1 AS n", nil); err != nil || row["n"] != 1 {
		t.Fatalf("fallback RunSingle() = %v, %v; want {n:1}, nil", row, err)
	}
	if got := idAnchorCensusReader(nil, nil); got != nil {
		t.Fatalf("idAnchorCensusReader(nil, nil) = %v, want nil", got)
	}
}

// TestDrainRegisterGraphBackedGaugesNil covers provenance_gauge_wiring.go:
// with no read port and no orphan observer the gauge registration is a nil
// no-op, unchanged by the contract-typed parameter.
func TestDrainRegisterGraphBackedGaugesNil(t *testing.T) {
	got, err := registerGraphBackedGauges(nil, nil, nil, nil, drainEnv(nil), nil)
	if err != nil || got != nil {
		t.Fatalf("registerGraphBackedGauges(nil...) = %v, %v; want nil, nil", got, err)
	}
}

// TestDrainSearchVectorBuildRunnerDisabled covers
// search_vector_build_wiring.go: without an embedder configured the builder
// stays off, so the semanticsearch default-config path is never reached.
func TestDrainSearchVectorBuildRunnerDisabled(t *testing.T) {
	runner, err := searchVectorBuildRunnerFor(nil, drainEnv(nil), nil, nil)
	if err != nil || runner != nil {
		t.Fatalf("searchVectorBuildRunnerFor(disabled) = %v, %v; want nil, nil", runner, err)
	}
}

// TestDrainSearchVectorBuildRunnerWiresDefaults covers the enabled leg of the
// same file: the runner carries the semanticsearch default hybrid config
// fields the old query forwarder used to supply.
func TestDrainSearchVectorBuildRunnerWiresDefaults(t *testing.T) {
	getenv := drainEnv(map[string]string{searchembedruntime.EnvLocalEmbedder: "hash"})
	runner, err := searchVectorBuildRunnerFor(nil, getenv, nil, nil)
	if err != nil {
		t.Fatalf("searchVectorBuildRunnerFor() error = %v", err)
	}
	if runner == nil {
		t.Fatal("searchVectorBuildRunnerFor() = nil, want a runner for the hash embedder")
	}
	defaults := semanticsearch.DefaultPersistedLocalSemanticSearchHybridConfig()
	if runner.Config.ProviderProfileID != defaults.ProviderProfileID ||
		runner.Config.SourceClass != defaults.SourceClass ||
		runner.Config.EmbeddingModelID != defaults.EmbeddingModelID ||
		runner.Config.VectorIndexVersion != defaults.VectorIndexVersion {
		t.Fatalf("runner config = %+v, want identity fields of %+v", runner.Config, defaults)
	}
}
