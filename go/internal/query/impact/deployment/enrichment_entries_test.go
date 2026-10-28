// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

type stubDeploymentContentStore struct {
	querycontract.ContentStore
	entities map[string][]querycontract.EntityContent
	err      error
}

func (s stubDeploymentContentStore) ListRepoEntities(_ context.Context, repoID string, _ int) ([]querycontract.EntityContent, error) {
	return s.entities[repoID], s.err
}

func TestQueryProvisioningRepositoryCandidates(t *testing.T) {
	t.Parallel()

	t.Run("NilGraphAndBlankRepo", func(t *testing.T) {
		t.Parallel()
		if got, truncated, err := QueryProvisioningRepositoryCandidates(t.Context(), nil, "repo-a", 10); got != nil || truncated || err != nil {
			t.Fatalf("nil graph = (%v, %v, %v), want (nil, false, nil)", got, truncated, err)
		}
		reader := graph.FakeGraphReader{}
		if got, truncated, err := QueryProvisioningRepositoryCandidates(t.Context(), reader, "  ", 10); got != nil || truncated || err != nil {
			t.Fatalf("blank repo = (%v, %v, %v), want (nil, false, nil)", got, truncated, err)
		}
	})
	t.Run("ReaderErrorPropagates", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, errors.New("backend down")
		}}
		if _, _, err := QueryProvisioningRepositoryCandidates(t.Context(), reader, "repo-a", 10); err == nil {
			t.Fatal("error = nil, want the reader failure propagated")
		}
	})
	t.Run("GroupsTypesAndReasonsPerRepo", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{
				{"repo_id": "repo-b", "repo_name": "org/consumer", "relationship_type": "USES_MODULE", "relationship_reason": "terraform"},
				{"repo_id": "repo-b", "repo_name": "org/consumer", "relationship_type": "DEPLOYS_FROM", "relationship_reason": "flux"},
				{"repo_id": "", "repo_name": "nameless"},
				{"repo_id": "repo-c", "repo_name": ""},
			}, nil
		}}
		got, truncated, err := QueryProvisioningRepositoryCandidates(t.Context(), reader, "repo-a", 10)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if truncated {
			t.Fatal("truncated = true, want false within the bound")
		}
		if len(got) != 1 || got[0].RepoID != "repo-b" {
			t.Fatalf("candidates = %+v, want only repo-b (blank ids/names dropped)", got)
		}
		if len(got[0].RelationshipTypes) != 2 || len(got[0].RelationshipReasons) != 2 {
			t.Fatalf("candidate = %+v, want both types and reasons grouped", got[0])
		}
	})
	t.Run("OverFetchDisclosesTruncation", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			if params["limit"] != 3 {
				t.Errorf("fetch limit = %v, want 3 (limit 2 plus the probe row)", params["limit"])
			}
			return []map[string]any{
				{"repo_id": "repo-b", "repo_name": "b"},
				{"repo_id": "repo-c", "repo_name": "c"},
				{"repo_id": "repo-d", "repo_name": "d"},
			}, nil
		}}
		got, truncated, err := QueryProvisioningRepositoryCandidates(t.Context(), reader, "repo-a", 2)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if !truncated {
			t.Fatal("truncated = false, want true (backend held more than the limit)")
		}
		if len(got) != 2 {
			t.Fatalf("len(candidates) = %d, want the trimmed limit", len(got))
		}
	})
}

func TestLoadProvisioningSourceChainsFromCandidates(t *testing.T) {
	t.Parallel()

	t.Run("EmptyAndNilContent", func(t *testing.T) {
		t.Parallel()
		if got, err := LoadProvisioningSourceChainsFromCandidates(t.Context(), stubDeploymentContentStore{}, nil); got != nil || err != nil {
			t.Fatalf("empty = (%v, %v), want (nil, nil)", got, err)
		}
		candidates := []ProvisioningRepositoryCandidate{{RepoID: "repo-b", RepoName: "org/consumer"}}
		if got, err := LoadProvisioningSourceChainsFromCandidates(t.Context(), nil, candidates); got != nil || err != nil {
			t.Fatalf("nil content = (%v, %v), want (nil, nil)", got, err)
		}
	})
	t.Run("BuildsChainsFromEntities", func(t *testing.T) {
		t.Parallel()
		content := stubDeploymentContentStore{entities: map[string][]querycontract.EntityContent{
			"repo-b": {terraformEntity("TerraformModule", "infra/main.tf", map[string]any{"source": "./modules/db"})},
		}}
		candidates := []ProvisioningRepositoryCandidate{{
			RepoID: "repo-b", RepoName: "org/consumer",
			RelationshipTypes: []string{"USES_MODULE"}, RelationshipReasons: []string{"terraform"},
		}}
		got, err := LoadProvisioningSourceChainsFromCandidates(t.Context(), content, candidates)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(got) != 1 {
			t.Fatalf("chains = %v, want one entry", got)
		}
		if got[0]["repository"] != "org/consumer" || got[0]["repo_id"] != "repo-b" {
			t.Fatalf("chain = %v, want candidate identity carried over", got[0])
		}
		modules, _ := got[0]["modules"].([]string)
		if len(modules) != 1 || modules[0] != "modules/db" {
			t.Fatalf("modules = %v, want the normalized module evidence", got[0]["modules"])
		}
	})
	t.Run("ContentErrorPropagates", func(t *testing.T) {
		t.Parallel()
		content := stubDeploymentContentStore{err: errors.New("store down")}
		candidates := []ProvisioningRepositoryCandidate{{RepoID: "repo-b"}}
		if _, err := LoadProvisioningSourceChainsFromCandidates(t.Context(), content, candidates); err == nil {
			t.Fatal("error = nil, want the store failure propagated")
		}
	})
}

func TestLoadConsumerRepositoryEnrichmentFromCandidates(t *testing.T) {
	t.Parallel()

	consumerCandidate := ProvisioningRepositoryCandidate{
		RepoID: "repo-b", RepoName: "org/consumer",
		RelationshipTypes: []string{"USES_MODULE"}, RelationshipReasons: []string{"terraform"},
	}

	t.Run("MergesCandidatesAndBackfillsNames", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"repo_id": "repo-b", "repo_name": "org/consumer-renamed"}}, nil
		}}
		nameless := consumerCandidate
		nameless.RepoName = ""
		got, truncated, err := LoadConsumerRepositoryEnrichmentFromCandidates(
			t.Context(), reader, nil, "repo-a", "Checkout Service",
			[]string{"checkout.example.test"}, 10,
			[]ProvisioningRepositoryCandidate{nameless}, false, false,
		)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if truncated {
			t.Fatal("truncated = true, want false (no bound hit)")
		}
		if len(got) != 1 {
			t.Fatalf("consumers = %v, want the one candidate", got)
		}
		if got[0]["repository"] != "org/consumer-renamed" {
			t.Fatalf("repository = %v, want the backfilled display name", got[0]["repository"])
		}
		kinds, _ := got[0]["consumer_kinds"].([]string)
		if len(kinds) == 0 || kinds[0] != "graph_provisioning_consumer" {
			t.Fatalf("consumer kinds = %v, want the graph consumer kind", kinds)
		}
	})
	t.Run("TinyLimitDisclosesTruncation", func(t *testing.T) {
		t.Parallel()
		// The doc comment calls out limit in {1,2,3} as the only
		// production-unreachable shape reaching the post-bound cap: with
		// five hostnames the affinity bound (4) already fits, so the
		// limit-2 slice below is what discloses the drop.
		hostnames := []string{"a.example.test", "b.example.test", "c.example.test", "d.example.test", "e.example.test"}
		reader := graph.FakeGraphReader{}
		_, truncated, err := LoadConsumerRepositoryEnrichmentFromCandidates(
			t.Context(), reader, nil, "repo-a", "Checkout Service",
			hostnames, 2, nil, false, false,
		)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if !truncated {
			t.Fatal("truncated = false, want true (the limit-2 slice dropped hostnames)")
		}
	})
	t.Run("CarriesUpstreamTruncation", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{}
		_, truncated, err := LoadConsumerRepositoryEnrichmentFromCandidates(
			t.Context(), reader, nil, "repo-a", "svc", nil, 10, nil, true, false,
		)
		if err != nil || !truncated {
			t.Fatalf("truncated = %v (err %v), want an upstream signal carried through", truncated, err)
		}
	})
}

func TestDeliveryPathHelpers(t *testing.T) {
	t.Parallel()

	t.Run("BuildNormalizedDeliveryPathsDedupes", func(t *testing.T) {
		t.Parallel()
		sources := []map[string]any{
			{"repo_name": "org/api", "repo_id": "repo-a"},
			{"repo_name": "org/api", "repo_id": "repo-a"},
			{"repo_name": "", "repo_id": ""},
		}
		got := BuildNormalizedDeliveryPaths(sources, nil, nil, []string{"img:1"}, nil, map[string]any{})
		if len(got) != 2 {
			t.Fatalf("paths = %v, want the deduped deployment source and image ref", got)
		}
		if got[0]["type"] != "deployment_source" || got[0]["target"] != "org/api" {
			t.Fatalf("first path = %v, want the shaped deployment source", got[0])
		}
	})
	t.Run("EvidencePathsStamped", func(t *testing.T) {
		t.Parallel()
		evidence := map[string]any{
			"delivery_paths": []map[string]any{{"path": "charts/app"}},
			"artifacts":      []map[string]any{{"resolved_id": "art-1"}},
		}
		got := DeploymentEvidenceDeliveryPaths(evidence)
		if len(got) != 2 {
			t.Fatalf("paths = %v, want both families", got)
		}
		if got[0]["type"] != "repository_delivery_artifact" {
			t.Fatalf("first path type = %v, want the default stamp", got[0]["type"])
		}
		if got[1]["type"] != "deployment_evidence" {
			t.Fatalf("second path type = %v, want deployment_evidence", got[1]["type"])
		}
	})
	t.Run("IdentityPredicates", func(t *testing.T) {
		t.Parallel()
		if genericDeliveryPathHasIdentity(map[string]any{}) {
			t.Fatal("empty entry has identity, want false")
		}
		if !genericDeliveryPathHasIdentity(map[string]any{"target_id": "x"}) {
			t.Fatal("target_id entry has no identity, want true")
		}
		if !repositoryDeliveryArtifactHasIdentity(map[string]any{"workflow_name": "ci"}) {
			t.Fatal("workflow entry has no artifact identity, want true")
		}
		if !deploymentEvidenceDeliveryPathHasIdentity(map[string]any{"source_repo_id": "r"}) {
			t.Fatal("source repo entry has no evidence identity, want true")
		}
	})
	t.Run("NormalizedDeliveryPathKey", func(t *testing.T) {
		t.Parallel()
		if got := NormalizedDeliveryPathKey(map[string]any{"type": "deployment_source", "target": "svc"}); got != "deployment_source|svc" {
			t.Fatalf("key = %q", got)
		}
		if got := NormalizedDeliveryPathKey(map[string]any{"type": "k8s_resource", "target_id": "uid-1", "kind": "Deployment"}); got != "k8s_resource|uid-1|Deployment" {
			t.Fatalf("key = %q", got)
		}
		if got := NormalizedDeliveryPathKey(map[string]any{"type": "image_ref", "target": "img:1"}); got != "image_ref|img:1" {
			t.Fatalf("key = %q", got)
		}
	})
}

func TestFluxBindingHelpers(t *testing.T) {
	t.Parallel()

	t.Run("ResultAccessors", func(t *testing.T) {
		t.Parallel()
		result := fluxDeploymentSourceTargetBindingResult{
			rows:              []map[string]any{{"source_id": "a"}},
			firstHopCount:     3,
			firstHopSaturated: true,
		}
		if len(result.Rows()) != 1 || result.FirstHopCount() != 3 || !result.FirstHopSaturated() {
			t.Fatalf("result = %+v, want accessors to report rows, count, saturation", result)
		}
		empty := fluxDeploymentSourceTargetBindingResult{}
		if empty.FirstHopSaturated() || len(empty.Rows()) != 0 {
			t.Fatalf("empty result = %+v, want zero values", empty)
		}
	})
	t.Run("FetchSaturationAndRows", func(t *testing.T) {
		t.Parallel()
		calls := 0
		reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			calls++
			if strings.Contains(cypher, "UNWIND $artifact_ids") {
				return []map[string]any{{"source_id": "repo-a", "target_id": "repo-b"}}, nil
			}
			return []map[string]any{{"artifact_id": "art-1"}, {"artifact_id": "art-2"}}, nil
		}}
		unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
		result, err := FetchFluxDeploymentSourceTargetBindings(t.Context(), reader, "repo-b", []string{"repo-a"}, 3, unscoped)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		// Two first-hop rows under the limit of three expand into the
		// binding rows; saturation stays off.
		if result.FirstHopCount() != 2 || result.FirstHopSaturated() {
			t.Fatalf("result = %+v, want count 2 without saturation", result)
		}
		if len(result.Rows()) != 1 || calls != 2 {
			t.Fatalf("rows = %v (calls %d), want the expansion rows from both reads", result.Rows(), calls)
		}
	})
	t.Run("FetchSaturatedSkipsExpansion", func(t *testing.T) {
		t.Parallel()
		calls := 0
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			calls++
			return []map[string]any{{"artifact_id": "art-1"}, {"artifact_id": "art-2"}}, nil
		}}
		unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
		result, err := FetchFluxDeploymentSourceTargetBindings(t.Context(), reader, "repo-b", []string{"repo-a"}, 2, unscoped)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if result.FirstHopCount() != 2 || !result.FirstHopSaturated() {
			t.Fatalf("result = %+v, want count 2 with saturation at the limit", result)
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1 (saturation skips the expansion read)", calls)
		}
	})
	t.Run("FetchReaderErrorPropagates", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, errors.New("backend down")
		}}
		if _, err := FetchFluxDeploymentSourceTargetBindings(t.Context(), reader, "repo-b", []string{"repo-a"}, 10, querycontract.RepositoryAccessFilter{AllScopes: true}); err == nil {
			t.Fatal("error = nil, want the reader failure propagated")
		}
	})
	t.Run("AttachBindings", func(t *testing.T) {
		t.Parallel()
		sources := []map[string]any{
			{"relationship_type": "DEPLOYS_FROM", "source_id": "repo-a", "target_id": "repo-b"},
			{"relationship_type": "OTHER", "source_id": "repo-a", "target_id": "repo-b"},
		}
		bindings := []map[string]any{
			{"source_id": "repo-a", "target_id": "repo-b", "flux_git_repository_namespace": "flux-system", "flux_git_repository_name": "fleet"},
			{"source_id": "repo-a", "target_id": "repo-b", "flux_git_repository_namespace": "", "flux_git_repository_name": "nameless"},
		}
		got := AttachFluxDeploymentSourceTargetBindings(sources, bindings, false)
		if len(got) != 2 {
			t.Fatalf("sources = %v, want both carried over", got)
		}
		attached, ok := got[0]["flux_git_repository_bindings"].([]map[string]any)
		if !ok || len(attached) != 1 {
			t.Fatalf("bindings = %v, want the one qualified binding attached", got[0]["flux_git_repository_bindings"])
		}
		if attached[0]["namespace"] != "flux-system" || attached[0]["name"] != "fleet" {
			t.Fatalf("binding = %v, want the fleet identity", attached[0])
		}
	})
	t.Run("AttachSaturatedMarksSources", func(t *testing.T) {
		t.Parallel()
		sources := []map[string]any{{"relationship_type": "DEPLOYS_FROM", "source_id": "a", "target_id": "b"}}
		got := AttachFluxDeploymentSourceTargetBindings(sources, nil, true)
		if got[0]["flux_target_bindings_saturated"] != true {
			t.Fatalf("source = %v, want the saturation mark without binding rows", got[0])
		}
	})
	t.Run("TallyAsMap", func(t *testing.T) {
		t.Parallel()
		tally := FluxTargetAttributionTally{Ambiguous: 1, Linked: 2, Missing: 3, Saturated: 4, Unsupported: 5}
		got := tally.AsMap()
		for key, want := range map[string]any{"ambiguous": 1, "linked": 2, "missing": 3, "saturated": 4, "unsupported": 5} {
			if got[key] != want {
				t.Fatalf("tally[%q] = %v, want %v", key, got[key], want)
			}
		}
	})
	t.Run("ChartPathRoot", func(t *testing.T) {
		t.Parallel()
		if !fluxHelmReleaseChartIsPathRoot(map[string]any{"source_ref_kind": "GitRepository"}) {
			t.Fatal("GitRepository source must treat chart as a path")
		}
		if fluxHelmReleaseChartIsPathRoot(map[string]any{"source_ref_kind": "HelmRepository"}) {
			t.Fatal("HelmRepository source must treat chart as a name")
		}
		if fluxHelmReleaseChartIsPathRoot(map[string]any{}) {
			t.Fatal("absent source kind must not claim a path root")
		}
	})
}

func TestDeploymentStoryHelpers(t *testing.T) {
	t.Parallel()

	t.Run("JoinOrNone", func(t *testing.T) {
		t.Parallel()
		if got := JoinOrNone(nil); got != "none" {
			t.Fatalf("JoinOrNone(nil) = %q, want none", got)
		}
		if got := JoinOrNone([]string{"a"}); got == "none" {
			t.Fatalf("JoinOrNone([a]) = %q, want the joined values", got)
		}
	})
	t.Run("OverallConfidenceTiers", func(t *testing.T) {
		t.Parallel()
		if confidence, basis := DeploymentOverallConfidence(nil, nil, nil, true); confidence != 0.95 || basis != "live_runtime_observation" {
			t.Fatalf("live = (%v, %q)", confidence, basis)
		}
		if confidence, basis := DeploymentOverallConfidence(
			[]map[string]any{{"materialization_confidence": 0.8}}, nil, nil, false,
		); confidence != 0.8 || basis != "materialized_runtime_instances" {
			t.Fatalf("instances = (%v, %q)", confidence, basis)
		}
		if confidence, basis := DeploymentOverallConfidence(nil, []map[string]any{{"confidence": 0.6}}, nil, false); confidence != 0.6 {
			t.Fatalf("sources = (%v, %q)", confidence, basis)
		}
		if confidence, basis := DeploymentOverallConfidence(nil, nil, []string{"prod"}, false); confidence != 0.45 || basis != "config_only_evidence" {
			t.Fatalf("config = (%v, %q)", confidence, basis)
		}
		if confidence, basis := DeploymentOverallConfidence(nil, nil, nil, false); confidence != 0 || basis != "no_deployment_evidence" {
			t.Fatalf("empty = (%v, %q)", confidence, basis)
		}
	})
}
