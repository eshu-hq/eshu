// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
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
	allAccess := repositoryAccessFilter{allScopes: true}
	entityCypher, _ := buildResolveEntityGraphQuery(resolveEntityRequest{
		Name:   "proof",
		RepoID: "proof-repository",
	}, 10, allAccess)
	codeCypher, _ := buildSearchGraphEntitiesQuery(
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
	workloadKind, ok := graphEntityKindByKey("services")
	if !ok {
		panic("services graph entity kind is not registered")
	}
	selectedResource := &resourceInvestigationCandidate{
		ID:     "proof-resource",
		Labels: []string{"CloudResource"},
	}
	resourceReq := resourceInvestigationRequest{MaxDepth: 3, Limit: 10}
	resourceSelectorReq := resourceInvestigationRequest{
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
		"QP-CODE-IMPORT-ROWS-REPOSITORY": directImportRowsCypher(importDependencyRequest{
			RepoID:     "proof-repository",
			SourceFile: "proof.go",
		}),
		"QP-CODE-IMPORT-PACKAGES": packageImportRowsCypher(importDependencyRequest{
			QueryType:    "package_imports",
			RepoID:       "proof-repository",
			SourceModule: "proof.source",
		}, []map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/proof.py"}}),
		"QP-CODE-IMPORT-SOURCE-MODULE-FILES": sourceModuleFilesCypher(importDependencyRequest{
			RepoID:       "proof-repository",
			SourceModule: "proof.source",
		}),
		"QP-CODE-IMPORT-TARGET-MODULE-FILES": targetModuleFilesCypher(importDependencyRequest{
			RepoID:       "proof-repository",
			TargetModule: "proof.target",
		}),
		"QP-CODE-IMPORT-SOURCE-MODULE-ROWS": sourceModuleImportRowsCypher(importDependencyRequest{
			RepoID:       "proof-repository",
			SourceModule: "proof.source",
		}, []map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/proof.py"}}),
		"QP-CODE-IMPORT-CROSS-MODULE-CALLS": crossModuleCallRowsCypher(importDependencyRequest{
			QueryType:    "cross_module_calls",
			RepoID:       "proof-repository",
			SourceModule: "proof.source",
			TargetModule: "proof.target",
		},
			[]map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/proof.py"}},
			[]map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/target.py"}},
		),
		"QP-ENTITY-MAP-RESOLVE-REPOSITORY": entityMapNodeResolverQuery(
			"Repository",
			"id",
			"proof-repository",
			"id",
			0,
			51,
		).cypher,
		"QP-ENTITY-MAP-DIRECT-REPOSITORY": entityMapDirectTraversalCypher(
			entityMapCandidate{AnchorLabel: "Repository", AnchorProperty: "id"},
			entityMapTraversalSpec{direction: "outgoing", relationships: []string{"DEPENDS_ON"}, minHops: 1, maxHops: 1},
		),
		"QP-ENTITY-MAP-BOUNDED-REPOSITORY": entityMapVariableTraversalCypher(
			entityMapCandidate{AnchorLabel: "Repository", AnchorProperty: "id"},
			entityMapTraversalSpec{direction: "outgoing", relationships: []string{"DEPENDS_ON"}, minHops: 2, maxHops: 3},
		),
		"QP-CLOUD-RESOURCE-LIST-HYDRATION": cloudCypher,
		"QP-CALL-GRAPH-HUBS":               mustCallGraphMetricsEdgesCypher("proof-repository"),
		"QP-CALL-GRAPH-RECURSIVE":          mustCallGraphMetricsEdgesCypher("proof-repository"),
		"QP-GRAPH-ENTITY-COUNT":            graphEntityKindCountCypher(workloadKind),
		"QP-GRAPH-ENTITY-LIST":             graphEntityKindListCypher(workloadKind, true),
		"QP-WORKLOAD-RESOLVE-PROPERTY":     workloadPropertyCypher,
		"QP-WORKLOAD-RESOLVE-RELATIONSHIP": workloadRelationshipCypher,
		"QP-RESOURCE-INVESTIGATION-WORKLOADS": resourceInvestigationWorkloadsCypher(
			selectedResource,
		),
		"QP-RESOURCE-INVESTIGATION-SELECTOR": resourceInvestigationSelectorLabelCypher(
			resourceSelectorReq,
			allAccess,
			"CloudResource",
			resourceInvestigationExactSelectorPredicates,
		),
		"QP-RESOURCE-INVESTIGATION-INSTANCE-WORKLOADS": resourceInvestigationInstanceWorkloadsCypher(),
		"QP-RESOURCE-INVESTIGATION-REPO-PATHS": resourceInvestigationRepoPathsCypher(
			resourceReq,
			selectedResource,
			"outgoing",
		),
	}
}

func mustCallGraphMetricsEdgesCypher(repoID string) string {
	cypher, _ := callGraphMetricsEdgesCypher(repoID)
	return cypher
}
