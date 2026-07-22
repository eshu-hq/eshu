// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveFluxDeploymentSourceTargetBindings proves the exact emitted
// first-hop and target-expansion queries against both repository-pinned graph
// backends. It is opt-in because its caller owns backend lifecycle.
func TestLiveFluxDeploymentSourceTargetBindings(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_5540_FLUX_BINDINGS_LIVE")) == "" {
		t.Skip("set ESHU_5540_FLUX_BINDINGS_LIVE=1 to run the live Flux binding proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	backend := strings.TrimSpace(os.Getenv("ESHU_5540_BACKEND"))
	if uri == "" || database == "" || (backend != "nornicdb" && backend != "neo4j") {
		t.Fatal("ESHU_NEO4J_URI, ESHU_NEO4J_DATABASE, and ESHU_5540_BACKEND=nornicdb|neo4j are required")
	}
	auth := neo4jdriver.NoAuth()
	if username := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USERNAME")); username != "" {
		auth = neo4jdriver.BasicAuth(username, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	writer := func(cypher string, params map[string]any) {
		t.Helper()
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
		defer func() { _ = session.Close(ctx) }()
		result, runErr := session.Run(ctx, cypher, params)
		if runErr == nil {
			_, runErr = result.Consume(ctx)
		}
		if runErr != nil {
			t.Fatalf("%s write failed: %v\n%s", backend, runErr, cypher)
		}
	}
	reader := NewNeo4jReader(driver, database)

	const prefix = "issue5540-live-"
	sourceID, targetA, targetB := prefix+"source", prefix+"target-a", prefix+"target-b"
	artifactIDs := []string{
		prefix + "team-a", prefix + "team-b", prefix + "duplicate-a", prefix + "duplicate-b",
		prefix + "ambiguous-a", prefix + "ambiguous-b",
	}
	for i := 0; i < 51; i++ {
		artifactIDs = append(artifactIDs, prefix+fmt.Sprintf("saturated-%02d", i))
	}
	cleanup := func() {
		writer("MATCH (a:EvidenceArtifact) WHERE a.id IN $ids DETACH DELETE a", map[string]any{"ids": artifactIDs})
		writer("MATCH (r:Repository) WHERE r.id IN $ids DETACH DELETE r", map[string]any{"ids": []string{sourceID, targetA, targetB}})
	}
	cleanup()
	defer cleanup()
	for _, id := range []string{sourceID, targetA, targetB} {
		writer("CREATE (:Repository {id: $id, name: $id})", map[string]any{"id": id})
	}
	seedArtifact := func(suffix, namespace, name, targetID string) {
		t.Helper()
		artifactID := prefix + suffix
		writer(`MATCH (source:Repository {id: $source_id})
CREATE (artifact:EvidenceArtifact {
  id: $artifact_id, relationship_type: 'DEPLOYS_FROM',
  evidence_kind: 'FLUX_GIT_REPOSITORY_SOURCE',
  flux_git_repository_namespace: $namespace,
  flux_git_repository_name: $name
})
CREATE (source)-[:HAS_DEPLOYMENT_EVIDENCE {relationship_type: 'DEPLOYS_FROM'}]->(artifact)`, map[string]any{
			"source_id": sourceID, "artifact_id": artifactID, "namespace": namespace, "name": name,
		})
		if targetID != "" {
			writer(`MATCH (artifact:EvidenceArtifact {id: $artifact_id})
MATCH (target:Repository {id: $target_id})
CREATE (artifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP {relationship_type: 'DEPLOYS_FROM'}]->(target)`, map[string]any{
				"artifact_id": artifactID, "target_id": targetID,
			})
		}
	}
	access := repositoryAccessFilter{
		allowedRepositoryIDs: []string{sourceID, targetA, targetB},
		allowed:              map[string]struct{}{sourceID: {}, targetA: {}, targetB: {}},
	}

	seedArtifact("team-a", "team-a", "app-source", targetA)
	seedArtifact("team-b", "team-b", "app-source", targetB)
	seedArtifact("duplicate-a", "team-dupe", "app-source", targetA)
	seedArtifact("duplicate-b", "team-dupe", "app-source", targetA)
	seedArtifact("ambiguous-a", "team-amb", "app-source", targetA)
	seedArtifact("ambiguous-b", "team-amb", "app-source", targetB)
	rowsA, err := fetchFluxDeploymentSourceTargetBindings(ctx, reader, targetA, []string{sourceID}, 51, access)
	if err != nil {
		t.Fatal(err)
	}
	rowsB, err := fetchFluxDeploymentSourceTargetBindings(ctx, reader, targetB, []string{sourceID}, 51, access)
	if err != nil {
		t.Fatal(err)
	}
	if rowsA.firstHopSaturated || rowsB.firstHopSaturated || rowsA.firstHopCount != 6 || rowsB.firstHopCount != 6 {
		t.Fatalf("%s namespace seed first hops = %#v / %#v", backend, rowsA, rowsB)
	}
	sources := attachFluxDeploymentSourceTargetBindings([]map[string]any{
		{"relationship_type": "DEPLOYS_FROM", "source_id": sourceID, "target_id": targetA},
		{"relationship_type": "DEPLOYS_FROM", "source_id": sourceID, "target_id": targetB},
	}, append(rowsA.rows, rowsB.rows...), false)
	controller := func(namespace string) map[string]any {
		return map[string]any{"controller_kind": "flux_kustomization", "repo_id": sourceID, "source_ref_kind": "GitRepository", "source_ref_name": "app-source", "namespace": namespace}
	}
	explicit, defaulted, duplicate, ambiguous, missing := controller("wrong"), controller("team-b"), controller("team-dupe"), controller("team-amb"), controller("")
	explicit["source_ref_namespace"] = "team-a"
	tally := bindFluxControllersToCrossRepoTargets([]map[string]any{explicit, defaulted, duplicate, ambiguous, missing}, sources)
	if tally != (fluxTargetAttributionTally{Linked: 3, Ambiguous: 1, Missing: 1}) ||
		StringVal(explicit, "flux_target_repo_id") != targetA ||
		StringVal(defaulted, "flux_target_repo_id") != targetB ||
		StringVal(duplicate, "flux_target_repo_id") != targetA ||
		StringVal(ambiguous, "flux_target_repo_id") != "" {
		t.Fatalf("%s namespace/ambiguity result: tally=%#v controllers=%#v", backend, tally, []map[string]any{explicit, defaulted, duplicate, ambiguous, missing})
	}

	cleanup()
	for _, id := range []string{sourceID, targetA, targetB} {
		writer("CREATE (:Repository {id: $id, name: $id})", map[string]any{"id": id})
	}
	for i := 0; i < 51; i++ {
		targetID := targetA
		if i == 50 {
			targetID = ""
		}
		seedArtifact(fmt.Sprintf("saturated-%02d", i), "flux-system", fmt.Sprintf("source-%02d", i), targetID)
	}
	for _, targetID := range []string{targetA, targetB} {
		got, fetchErr := fetchFluxDeploymentSourceTargetBindings(ctx, reader, targetID, []string{sourceID}, 51, access)
		if fetchErr != nil {
			t.Fatal(fetchErr)
		}
		if !got.firstHopSaturated || got.firstHopCount != 51 || len(got.rows) != 0 {
			t.Fatalf("%s target %s saturation = %#v, want 51-row first-hop lower bound and zero attribution", backend, targetID, got)
		}
	}
}
