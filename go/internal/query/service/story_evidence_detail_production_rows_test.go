// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// storyHandleRowContract is what the handles view does with each key of one
// story list's production rows: kept keys ship with the value of the full row
// (identity keys and row truth labels), dropped keys do not ship. A production
// row key in neither list fails the test, so a key a builder adds later needs a
// decision.
type storyHandleRowContract struct {
	kept    []string
	dropped []string
}

// storyHandleRowContracts names the decision per projected story list. The
// rows the test checks come from the production builders, not a fixture.
var storyHandleRowContracts = map[string]storyHandleRowContract{
	"upstream_dependencies": {
		kept:    []string{"resolved_id", "relationship_type", "source", "source_repo_id", "target", "target_repo_id", "target_id", "confidence"},
		dropped: []string{"source_repo_canonical_id", "source_repo_scope_key", "evidence_count", "rationale", "modules"},
	},
	"dependents": {
		kept:    []string{"repo_id", "repository", "relationship_types"},
		dropped: []string{"relationship_reasons"},
	},
	"consumer_repositories": {
		kept:    []string{"repo_id", "repository", "consumer_kinds", "graph_relationship_types", "evidence_kinds"},
		dropped: []string{"graph_relationship_reasons", "matched_values", "sample_paths"},
	},
	"provisioning_source_chains": {
		kept:    []string{"repo_id", "repository", "relationship_types", "evidence_kinds"},
		dropped: []string{"relationship_reasons", "sample_paths", "modules", "config_paths"},
	},
	"hostnames": {
		kept:    []string{"hostname", "environment"},
		dropped: []string{"relative_path", "reason"},
	},
	"entrypoints": {
		kept:    []string{"type", "target", "environment", "visibility"},
		dropped: []string{"relative_path", "reason"},
	},
	"entrypoint_candidates": {
		kept:    []string{"candidate", "classification", "reason"},
		dropped: []string{"relative_path"},
	},
	"network_paths": {
		kept:    []string{"path_type", "from_type", "from", "to", "platform_kind", "environment", "visibility"},
		dropped: []string{"to_type", "reason"},
	},
	"cloud_resources": {
		kept: []string{
			"id", "name", "kind", "resource_type",
			"relationship_basis", "resolution_mode", "evidence_source", "service_anchor_source", "service_anchor_reason",
		},
		dropped: []string{
			"provider", "environment", "resource_id", "arn", "account_id", "region",
			"source_fact_id", "stable_fact_key", "source_system", "source_record_id", "collector_kind",
		},
	},
	"uncorrelated_cloud_resources": {
		kept: []string{
			"id", "name", "kind", "resource_type",
			"candidate_status", "missing_relationship", "service_anchor_status", "service_anchor_reason",
		},
		dropped: []string{
			"provider", "environment", "source", "config_path", "resource_service", "resource_category",
			"resource_id", "arn", "account_id", "region", "service_kind",
		},
	},
}

// productionShapedStoryContext builds a small workload context whose story
// lists come from the production row builders: BuildGraphDependents,
// deployment.LoadConsumerRepositoryEnrichmentFromCandidates,
// deployment.LoadProvisioningSourceChainsFromCandidates, the hostname,
// entrypoint, entrypoint-candidate, and network-path builders, and the
// materialized cloud-resource reader (or, with candidatesOnly, the uncorrelated
// candidate reader) over a fake graph row carrying every column either Cypher
// returns.
func productionShapedStoryContext(t *testing.T, candidatesOnly bool) map[string]any {
	t.Helper()
	ctx := context.Background()
	provisioning := []deployment.ProvisioningRepositoryCandidate{
		{RepoID: "repo-terraform", RepoName: "terraform-runtime", RelationshipTypes: []string{"PROVISIONS_DEPENDENCY_FOR"}, RelationshipReasons: []string{"terraform_module_source"}},
		{RepoID: "repo-gitops", RepoName: "deployment-charts", RelationshipTypes: []string{"DEPLOYS_FROM"}, RelationshipReasons: []string{"helm_values_reference"}},
	}
	consumers, _, err := deployment.LoadConsumerRepositoryEnrichmentFromCandidates(
		ctx, nil, content.FakePortContentStore{}, "repo-checkout-service", "checkout-service",
		nil, querycontract.DefaultIndirectEvidenceSearchLimit, provisioning, false, false,
	)
	if err != nil {
		t.Fatalf("LoadConsumerRepositoryEnrichmentFromCandidates() error = %v", err)
	}
	chains, err := deployment.LoadProvisioningSourceChainsFromCandidates(ctx, content.FakePortContentStore{}, provisioning)
	if err != nil {
		t.Fatalf("LoadProvisioningSourceChainsFromCandidates() error = %v", err)
	}
	evidence := QueryEvidence{
		Hostnames: []HostnameEvidence{{
			Hostname: "checkout.staging.example.test", Environment: "staging",
			RelativePath: "charts/checkout/values-staging.yaml", Reason: "exact_hostname_candidate_reference",
		}},
		DocsRoutes: []DocsRouteEvidence{{Route: "/staging/docs", RelativePath: "docs/staging/index.md", Reason: "docs_route_reference"}},
		EntrypointCandidates: []EntrypointCandidateEvidence{{
			Candidate: "checkout.settings.queue.endpoint", Classification: "config_key",
			RelativePath: "charts/checkout/config/settings.yaml", Reason: "ambiguous_without_stronger_hostname_evidence",
		}},
	}
	workloadContext := map[string]any{
		"id": "workload:checkout-service", "name": "checkout-service", "kind": "service",
		"repo_id": "repo-checkout-service", "repo_name": "checkout-service",
		"instances": []map[string]any{{
			"instance_id": "inst-1", "platform_name": "eks-staging", "platform_kind": "eks", "environment": "staging",
			"materialization_status": "materialized",
		}},
		"dependents":                 BuildGraphDependents(provisioning),
		"consumer_repositories":      consumers,
		"provisioning_source_chains": chains,
		"hostnames":                  buildServiceHostnameRows(evidence.Hostnames),
		"entrypoint_candidates":      buildServiceEntrypointCandidateRows(evidence.EntrypointCandidates),
		"dependencies":               []map[string]any{{"target_name": "payments", "target_id": "workload:payments", "type": "DEPENDS_ON", "confidence": 0.8}},
		"deployment_evidence": map[string]any{"artifacts": []map[string]any{{
			"id": "artifact-1", "direction": "incoming", "relationship_type": "DEPLOYS_FROM", "resolved_id": "resolved-1",
			"confidence": 0.94, "evidence_count": 3, "rationale": "applicationset references the chart",
			"source_repo_id": "repo-gitops", "source_repo_name": "deployment-charts", "source_repo_canonical_id": "canonical-gitops",
			"source_repo_scope_key": "scope-gitops", "target_repo_id": "repo-checkout-service", "target_repo_name": "checkout-service",
		}}},
	}
	entrypoints := buildServiceEntrypoints(workloadContext, evidence)
	workloadContext["entrypoints"] = entrypoints
	workloadContext["network_paths"] = buildServiceNetworkPaths(workloadContext, entrypoints)

	cloudRow := map[string]any{
		"id": "cloud:aws:sqs:checkout", "name": "checkout-queue", "kind": "sqs_queue", "resource_type": "aws_sqs_queue",
		"provider": "aws", "environment": "staging", "resource_id": "checkout-queue", "arn": "arn:aws:sqs:us-east-1:000000000000:checkout-queue",
		"account_id": "000000000000", "region": "us-east-1", "resolution_mode": "exact_arn", "evidence_source": "terraform_state",
		"relationship_basis": "aws_resource_service_anchor", "service_anchor_source": "resource_tag", "service_anchor_reason": "service_tag_match",
		"source_fact_id": "fact-1", "stable_fact_key": "key-1", "source_system": "aws", "source_record_id": "record-1", "collector_kind": "aws_cloud",
		"source": "terraform_state", "config_path": "env/staging/sqs.tf", "resource_service": "sqs", "resource_category": "messaging",
		"service_kind": "sqs", "service_anchor_status": "weak",
	}
	reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		return []map[string]any{cloudRow}, nil
	}}
	if candidatesOnly {
		candidates, _, err := deployment.LoadUncorrelatedCloudResourceCandidatesBounded(ctx, reader, "checkout-service", serviceStoryItemLimit)
		if err != nil {
			t.Fatalf("LoadUncorrelatedCloudResourceCandidatesBounded() error = %v", err)
		}
		workloadContext["uncorrelated_cloud_resources"] = candidates
	} else {
		resources, err := deployment.LoadMaterializedServiceCloudResourceDependencies(ctx, reader, "repo-checkout-service", "workload:checkout-service", serviceStoryItemLimit)
		if err != nil {
			t.Fatalf("LoadMaterializedServiceCloudResourceDependencies() error = %v", err)
		}
		workloadContext["cloud_resources"] = resources
	}
	return workloadContext
}

// TestStoryHandlesKeepProductionRowTruthLabels proves that a handle row built
// from a production-shaped row keeps that row's identity keys and truth labels
// and drops only the keys named as bulk (#8798 review P2-1). An earlier check
// ran only over a fixture that invented labels, such as confidence on a
// provisioning chain, while the real chain row's relationship_types and
// evidence_kinds were dropped.
func TestStoryHandlesKeepProductionRowTruthLabels(t *testing.T) {
	t.Parallel()

	families := make([]string, 0, len(storyHandleRowContracts))
	for family := range storyHandleRowContracts {
		families = append(families, family)
	}
	sort.Strings(families)
	for _, candidatesOnly := range []bool{false, true} {
		full := BuildServiceStoryResponse("checkout-service", productionShapedStoryContext(t, candidatesOnly))
		handles := BuildServiceStoryResponse("checkout-service", productionShapedStoryContext(t, candidatesOnly))
		querycontract.ApplyStoryEvidenceDetail(handles, querycontract.ContextEvidenceDetailHandles)

		for _, family := range families {
			if (family == "cloud_resources" && candidatesOnly) || (family == "uncorrelated_cloud_resources" && !candidatesOnly) {
				continue // the enrichment ships one cloud family or the other, never both
			}
			contract := storyHandleRowContracts[family]
			fullRows := querycontract.MapSliceValue(full, family)
			handleRows := querycontract.MapSliceValue(handles, family)
			if len(fullRows) == 0 {
				t.Errorf("candidates_only=%v: the production builders produced no %s rows; the test no longer covers it", candidatesOnly, family)
				continue
			}
			if len(handleRows) != len(fullRows) {
				t.Errorf("%s: %d handle rows, want %d", family, len(handleRows), len(fullRows))
				continue
			}
			for i, row := range fullRows {
				for key, value := range row {
					switch {
					case slices.Contains(contract.kept, key):
						if got, ok := handleRows[i][key]; !ok || !reflect.DeepEqual(got, value) {
							t.Errorf("%s[%d].%s = %#v under handles, want %#v kept", family, i, key, got, value)
						}
					case slices.Contains(contract.dropped, key):
						if got, ok := handleRows[i][key]; ok {
							t.Errorf("%s[%d].%s = %#v under handles, want it dropped as bulk", family, i, key, got)
						}
					default:
						t.Errorf("%s[%d].%s is a production row key with no kept or dropped decision", family, i, key)
					}
				}
			}
		}
	}
}
