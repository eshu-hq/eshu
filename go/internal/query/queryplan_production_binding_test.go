// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

func TestHandlerQueryplanManifestBindsProductionBuilders(t *testing.T) {
	manifest, err := queryplan.LoadManifestFile("../queryplan/testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatalf("LoadManifestFile() error = %v", err)
	}
	manifest, err = queryplan.BindProductionCypher(manifest, handlerQueryplanProductionCypher())
	if err != nil {
		t.Fatalf("BindProductionCypher() error = %v", err)
	}
	statements, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend() error = %v", err)
	}
	if err := queryplan.ValidateManifest(manifest, statements); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
	if err := queryplan.ValidateManifestSources(manifest, "../../.."); err != nil {
		t.Fatalf("ValidateManifestSources() error = %v", err)
	}
}

func handlerQueryplanProductionCypher() map[string]string {
	allAccess := repositoryAccessFilter{AllScopes: true}
	entityCypher, _ := buildResolveEntityGraphQuery(resolveEntityRequest{
		Name:   "proof",
		RepoID: "proof-repository",
	}, 10, allAccess)
	codeCypher, _ := codemodel.BuildSearchGraphEntitiesQuery(
		"proof-repository",
		"proof",
		"",
		10,
		true,
		allAccess,
	)
	cloudCypher, _ := buildCloudResourceHydrationQuery([]CloudResourceListIdentity{{
		UID: "proof-cloud-resource", ResourceType: "proof-type",
	}})
	selectedResource := &impact.ResourceInvestigationCandidate{
		ID:     "proof-resource",
		Labels: []string{"CloudResource"},
	}
	resourceReq := impact.ResourceInvestigationRequest{MaxDepth: 3, Limit: 10}
	resourceSelectorReq := impact.ResourceInvestigationRequest{
		Query:        "proof-resource",
		ResourceType: "cloud",
		Limit:        10,
	}
	workloadPropertyCypher, workloadRelationshipCypher, _ := buildResolveWorkloadQueries(
		"proof",
		"proof-repository",
		10,
		allAccess,
	)

	return map[string]string{
		"QP-ENTITY-RESOLVE-REPOSITORY": entityCypher,
		"QP-CODE-SEARCH-REPOSITORY":    codeCypher,
		// access is set explicitly on every import-dependency request below.
		// Since #5167 batch 2a these builders render the caller's repository
		// grant, and repositoryAccessFilter's zero value is a SCOPED filter with
		// no grants -- so an unset access would silently repin these entries to
		// the scoped shape, while the plan operators each entry commits to
		// describe the repository-anchored shape a shared-key caller runs. The
		// scoped shapes are covered by the import-dependency variant family
		// (queryplan_import_dependencies_variants_test.go), which enumerates
		// both caller classes.
		"QP-CODE-IMPORT-ROWS-REPOSITORY": codemodel.DirectImportRowsCypher(codemodel.ImportDependencyRequest{
			RepoID:     "proof-repository",
			SourceFile: "proof.go",
			Access:     allAccess,
		}),
		"QP-CODE-IMPORT-PACKAGES": codemodel.PackageImportRowsCypher(codemodel.ImportDependencyRequest{
			QueryType:    "package_imports",
			RepoID:       "proof-repository",
			SourceModule: "proof.source",
			Access:       allAccess,
		}, []map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/proof.py"}}),
		"QP-CODE-IMPORT-SOURCE-MODULE-FILES": codemodel.SourceModuleFilesCypher(codemodel.ImportDependencyRequest{
			RepoID:       "proof-repository",
			SourceModule: "proof.source",
			Access:       allAccess,
		}),
		"QP-CODE-IMPORT-TARGET-MODULE-FILES": codemodel.TargetModuleFilesCypher(codemodel.ImportDependencyRequest{
			RepoID:       "proof-repository",
			TargetModule: "proof.target",
			Access:       allAccess,
		}),
		"QP-CODE-IMPORT-SOURCE-MODULE-ROWS": codemodel.SourceModuleImportRowsCypher(codemodel.ImportDependencyRequest{
			RepoID:       "proof-repository",
			SourceModule: "proof.source",
			Access:       allAccess,
		}, []map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/proof.py"}}),
		"QP-CODE-IMPORT-CROSS-MODULE-CALLS": codemodel.CrossModuleCallRowsCypher(
			codemodel.ImportDependencyRequest{
				QueryType:    "cross_module_calls",
				RepoID:       "proof-repository",
				SourceModule: "proof.source",
				TargetModule: "proof.target",
				Access:       allAccess,
			},
			[]map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/proof.py"}},
			[]map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/target.py"}},
		),
		"QP-ENTITY-MAP-RESOLVE-REPOSITORY": impact.EntityMapNodeResolverQuery(
			"Repository",
			"id",
			"proof-repository",
			"id",
			0,
			51,
		).Cypher,
		"QP-ENTITY-MAP-DIRECT-REPOSITORY": impact.EntityMapDirectTraversalCypher(
			impact.EntityMapCandidate{AnchorLabel: "Repository", AnchorProperty: "id"},
			impact.EntityMapTraversalSpec{Direction: "outgoing", Relationships: []string{"DEPENDS_ON"}, MinHops: 1, MaxHops: 1},
		),
		"QP-ENTITY-MAP-BOUNDED-REPOSITORY": impact.EntityMapVariableTraversalCypher(
			impact.EntityMapCandidate{AnchorLabel: "Repository", AnchorProperty: "id"},
			impact.EntityMapTraversalSpec{Direction: "outgoing", Relationships: []string{"DEPENDS_ON"}, MinHops: 2, MaxHops: 3},
		),
		"QP-CLOUD-RESOURCE-LIST-HYDRATION":   cloudCypher,
		"QP-SUPPLY-CHAIN-KUBERNETES-RUNTIME": supplyChainKubernetesRuntimeProbeCypher,
		"QP-CALL-GRAPH-HUBS":                 mustCallGraphMetricsEdgesCypher("proof-repository"),
		"QP-CALL-GRAPH-RECURSIVE":            mustCallGraphMetricsEdgesCypher("proof-repository"),
		"QP-GRAPH-ENTITY-COUNT":              graphEntityKindCountsCypher(graphEntityKinds),
		"QP-GRAPH-ENTITY-LIST":               graphEntityKindListCypher(graphEntityKinds[0], true),
		"QP-WORKLOAD-RESOLVE-PROPERTY":       workloadPropertyCypher,
		"QP-WORKLOAD-RESOLVE-RELATIONSHIP":   workloadRelationshipCypher,
		"QP-RESOURCE-INVESTIGATION-WORKLOADS": impact.ResourceInvestigationWorkloadsCypher(
			selectedResource,
		),
		"QP-RESOURCE-INVESTIGATION-SELECTOR": impact.ResourceInvestigationSelectorLabelCypher(
			resourceSelectorReq,
			allAccess,
			"CloudResource",
			impact.ResourceInvestigationExactSelectorPredicates,
		),
		"QP-RESOURCE-INVESTIGATION-INSTANCE-WORKLOADS": impact.ResourceInvestigationInstanceWorkloadsCypher(),
		"QP-RESOURCE-INVESTIGATION-REPO-PATHS": impact.ResourceInvestigationRepoPathsCypher(
			resourceReq,
			selectedResource,
			"outgoing",
		),
	}
}

func mustCallGraphMetricsEdgesCypher(repoID string) string {
	cypher, _ := codequery.CallGraphMetricsEdgesCypher(repoID)
	return cypher
}
