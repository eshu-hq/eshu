// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

func TestLegacyQueryplanManifestBindsProductionQueries(t *testing.T) {
	manifest, err := queryplan.LoadManifestFile("../queryplan/testdata/hot-cypher.yaml")
	if err != nil {
		t.Fatalf("LoadManifestFile() error = %v", err)
	}
	manifest, err = queryplan.BindProductionCypher(manifest, legacyQueryplanProductionCypher(t))
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

func legacyQueryplanProductionCypher(t *testing.T) map[string]string {
	t.Helper()
	packageRegistryDependencies, _ := packageRegistryDependenciesCypher("", "proof-version", "", "", 51)
	serviceResolve := captureLegacyQueryplanCypher(t, func(graphQuery *legacyQueryplanCaptureGraph) error {
		handler := &EntityHandler{Neo4j: graphQuery}
		_, err := handler.queryServiceWorkloadCandidates(
			context.Background(),
			"w.id = $service_id",
			"service_id",
			"workload:proof",
			serviceWorkloadSelector{ServiceID: "workload:proof"},
			"",
			serviceWorkloadCandidateLimit+1,
			"workload_id",
		)
		return err
	})
	serviceContext := captureLegacyQueryplanCypher(t, func(graphQuery *legacyQueryplanCaptureGraph) error {
		handler := &EntityHandler{Neo4j: graphQuery}
		_, err := handler.fetchWorkloadContextForOperation(
			context.Background(),
			"w.id = $workload_id",
			map[string]any{"workload_id": "workload:proof"},
			"workload_context",
		)
		return err
	})
	serviceRunsOn := captureLegacyQueryplanCypher(t, func(graphQuery *legacyQueryplanCaptureGraph) error {
		handler := &EntityHandler{Neo4j: graphQuery}
		_, err := handler.fetchWorkloadPlatformRows(
			context.Background(),
			"repository:proof",
			"workload:proof",
			[]map[string]any{{"instance_id": "instance:proof"}},
		)
		return err
	})
	serviceCloudDependencies := captureLegacyQueryplanCypher(t, func(graphQuery *legacyQueryplanCaptureGraph) error {
		_, err := loadMaterializedServiceCloudResourceDependencies(
			context.Background(),
			graphQuery,
			"repository:proof",
			"workload:proof",
			10,
		)
		return err
	})
	directRelationship, _ := nornicDBRelationshipStoryGraphCypher(
		relationshipStoryRequest{RelationshipType: "CALLS", Limit: 10},
		"entity:proof",
		"Function",
		"uid",
		"outgoing",
		repositoryAccessFilter{allScopes: true},
	)
	incomingRelationship, _ := nornicDBRelationshipStoryGraphCypher(
		relationshipStoryRequest{RelationshipType: "CALLS", Limit: 10},
		"entity:proof",
		"Function",
		"uid",
		"incoming",
		repositoryAccessFilter{allScopes: true},
	)
	transitiveRelationship, _ := nornicDBRelationshipStoryInheritanceDepthCypher(
		relationshipStoryRequest{MaxDepth: 5, Limit: 10},
		"entity:proof",
		"outgoing",
		"uid",
	)
	hostedRepositoryCount := captureLegacyQueryplanCypher(t, func(graphQuery *legacyQueryplanCaptureGraph) error {
		handler := &StatusHandler{Neo4j: graphQuery}
		_, err := handler.readHostedRepositoryCount(context.Background())
		return err
	})
	changeSurface := captureLegacyQueryplanCypher(t, func(graphQuery *legacyQueryplanCaptureGraph) error {
		handler := &ImpactHandler{Neo4j: graphQuery}
		_, _, err := handler.findChangeSurfaceImpactRows(
			context.Background(),
			changeSurfaceTargetCandidate{ID: "workload:proof", Labels: []string{"Workload"}},
			"",
			changeSurfaceLegacyDefaultDepth,
			10,
			repositoryAccessFilter{allScopes: true},
		)
		return err
	})
	fluxBindingsGraph := &legacyQueryplanCaptureGraph{runRows: func(run int) []map[string]any {
		if run == 0 {
			return []map[string]any{{"artifact_id": "artifact:proof"}}
		}
		return nil
	}}
	if _, err := fetchFluxDeploymentSourceTargetBindings(
		context.Background(),
		fluxBindingsGraph,
		"repository:target",
		[]string{"repository:source"},
		51,
		repositoryAccessFilter{allScopes: true},
	); err != nil {
		t.Fatalf("capture Flux deployment bindings Cypher: %v", err)
	}
	if len(fluxBindingsGraph.cypher) != 2 {
		t.Fatalf("captured Flux deployment binding Cypher count = %d, want 2", len(fluxBindingsGraph.cypher))
	}
	infraSearch := captureLegacyQueryplanCypher(t, func(graphQuery *legacyQueryplanCaptureGraph) error {
		handler := &InfraHandler{Neo4j: graphQuery}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v0/infra/resources/search",
			bytes.NewBufferString(`{"query":"proof","category":"cloud","limit":10}`),
		)
		response := httptest.NewRecorder()
		handler.searchResources(response, request)
		if response.Code != http.StatusOK {
			return fmt.Errorf("infra search status %d: %s", response.Code, response.Body.String())
		}
		return nil
	})
	sourceToolQueries := relationshipSourceToolBreakdownCyphers()
	if len(sourceToolQueries) != 2 {
		t.Fatalf("source-tool query count = %d, want 2", len(sourceToolQueries))
	}
	codeownersOwnershipList := codeownersOwnershipCyphers("proof-repository", -1, "", "", 51)[0].cypher
	codeownersOwnershipCursor := codeownersOwnershipCyphers("proof-repository", 1, "*.go", "@proof/team", 51)
	codeownersLastMatchOwner, _ := codeownersLastMatchOwnerCypher("proof-repository")
	return map[string]string{
		"QP-SC-DEPS":                                      forwardDependenciesCypher("proof"),
		"QP-SC-PKGREG-DEPS":                               packageRegistryDependencies,
		"QP-DEPLOY-CATALOG-ENV":                           catalogWorkloadEvidenceEnvironmentCypher,
		"QP-DEPLOY-CATALOG-WORKLOAD-REPO":                 catalogWorkloadRepoCypher,
		"QP-DEPLOY-CATALOG-WORKLOAD-INSTANCE":             catalogWorkloadInstanceEnvironmentCypher,
		"QP-SVC-RESOLVE":                                  serviceResolve,
		"QP-SVC-CONTEXT":                                  serviceContext,
		"QP-SVC-RUNS-ON":                                  serviceRunsOn,
		"QP-SVC-CLOUD-DEPS":                               serviceCloudDependencies,
		"QP-CODE-REL-STORY":                               directRelationship,
		"QP-CODE-REL-TRANSITIVE":                          transitiveRelationship,
		"QP-CODE-REL-STORY-INCOMING":                      incomingRelationship,
		"QP-CODE-REL-STORY-ANCHOR-COLLISION":              nornicDBRelationshipStoryAnchorLookupCypher("Function", "id", false),
		"QP-CODE-IMPORT-CYCLES":                           fileImportCycleEdgeRowsCypher(importDependencyRequest{QueryType: "file_import_cycles", RepoID: "proof-repository", Limit: 10}),
		"QP-READINESS-HOSTED":                             hostedRepositoryCount,
		"QP-IMPACT-CHANGE-SURFACE":                        changeSurface,
		"QP-IMPACT-FLUX-BINDINGS-FIRST-HOP":               fluxBindingsGraph.cypher[0],
		"QP-IMPACT-FLUX-BINDINGS-TARGET-EXPANSION":        fluxBindingsGraph.cypher[1],
		"QP-RELATIONSHIPS-CATALOG-COUNT":                  relationshipCountCypher(relationshipVerbByName["CALLS"]),
		"QP-RELATIONSHIPS-EDGES":                          relationshipEdgesCypher(relationshipVerbByName["CALLS"], repositoryAccessFilter{allScopes: true}),
		"QP-RELATIONSHIPS-CATALOG-SOURCE-TOOL-REPOSITORY": sourceToolQueries[0],
		"QP-RELATIONSHIPS-CATALOG-SOURCE-TOOL-INSTANCE":   sourceToolQueries[1],
		"QP-INFRA-RESOURCE-SEARCH":                        infraSearch,
		"QP-INFRA-RESOURCE-AGGREGATE": infraResourceAggregatePerLabelCypher(
			[]string{"CloudResource"},
			"",
			"RETURN head(labels(n)) AS bucket, count(n) AS bucket_count",
			"RETURN bucket, bucket_count",
		),
		"QP-CODEOWNERS-OWNERSHIP-LIST":           codeownersOwnershipList,
		"QP-CODEOWNERS-OWNERSHIP-CURSOR-ORDER":   codeownersOwnershipCursor[0].cypher,
		"QP-CODEOWNERS-OWNERSHIP-CURSOR-PATTERN": codeownersOwnershipCursor[1].cypher,
		"QP-CODEOWNERS-OWNERSHIP-CURSOR-REF":     codeownersOwnershipCursor[2].cypher,
		"QP-CODEOWNERS-LAST-MATCH-OWNER":         codeownersLastMatchOwner,
	}
}

type legacyQueryplanCaptureGraph struct {
	cypher  []string
	runRows func(int) []map[string]any
}

func (g *legacyQueryplanCaptureGraph) Run(
	_ context.Context,
	cypher string,
	_ map[string]any,
) ([]map[string]any, error) {
	run := len(g.cypher)
	g.cypher = append(g.cypher, cypher)
	if g.runRows != nil {
		return g.runRows(run), nil
	}
	return nil, nil
}

func (g *legacyQueryplanCaptureGraph) RunSingle(
	_ context.Context,
	cypher string,
	_ map[string]any,
) (map[string]any, error) {
	g.cypher = append(g.cypher, cypher)
	return nil, nil
}

func captureLegacyQueryplanCypher(
	t *testing.T,
	invoke func(*legacyQueryplanCaptureGraph) error,
) string {
	t.Helper()
	graphQuery := &legacyQueryplanCaptureGraph{}
	if err := invoke(graphQuery); err != nil {
		t.Fatalf("capture production Cypher: %v", err)
	}
	if len(graphQuery.cypher) != 1 {
		t.Fatalf("captured production Cypher count = %d, want 1", len(graphQuery.cypher))
	}
	return graphQuery.cypher[0]
}
