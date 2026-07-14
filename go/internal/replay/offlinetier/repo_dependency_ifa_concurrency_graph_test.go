// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func seedRepoDependencyIfaRepositories(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	odu ifa.Odu,
) {
	t.Helper()
	seen := make(map[string]struct{})
	for _, fact := range odu.Facts {
		if fact.FactKind != "repository" {
			continue
		}
		repoID := strings.TrimSpace(fmt.Sprint(fact.Payload["repo_id"]))
		if repoID == "" {
			continue
		}
		if _, duplicate := seen[repoID]; duplicate {
			continue
		}
		seen[repoID] = struct{}{}
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MERGE (repo:Repository {id: $id})
				ON CREATE SET repo.marker = $marker`,
			Parameters: map[string]any{
				"id":     repoID,
				"marker": repoDependencyConcurrencyMarker,
			},
		}); err != nil {
			t.Fatalf("seed Odù repository %q: %v", repoID, err)
		}
		owned, err := exec.count(
			ctx,
			`MATCH (repo:Repository {id: $id, marker: $marker}) RETURN count(repo)`,
			map[string]any{"id": repoID, "marker": repoDependencyConcurrencyMarker},
		)
		if err != nil {
			t.Fatalf("verify proof-owned Odù repository %q: %v", repoID, err)
		}
		if owned != 1 {
			t.Fatalf("Odù repository %q is not owned by the repo-dependency proof", repoID)
		}
	}
	if got, want := len(seen), 11; got != want {
		t.Fatalf("seeded repositories=%d, want %d", got, want)
	}
}

type repoDependencyIfaSnapshot struct {
	canonical          []string
	typedEdges         []string
	nodeCounts         map[string]int
	relationshipCounts map[string]int
}

func readRepoDependencyIfaSnapshot(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	artifactIDs []string,
) repoDependencyIfaSnapshot {
	t.Helper()
	queries := repoDependencyIfaNodeSnapshotQueries(artifactIDs)
	queries = append(queries, repoDependencyIfaRelationshipSnapshotQueries(artifactIDs)...)

	snapshot := repoDependencyIfaSnapshot{
		nodeCounts:         make(map[string]int),
		relationshipCounts: make(map[string]int),
	}
	for queryIndex, query := range queries {
		session := exec.driver.NewSession(ctx, exec.sessionConfig(neo4jdriver.AccessModeRead))
		result, err := session.Run(ctx, query.stmt.Cypher, query.stmt.Parameters)
		if err != nil {
			_ = session.Close(ctx)
			t.Fatalf("snapshot query %d: %v", queryIndex, err)
		}
		for result.Next(ctx) {
			values := result.Record().Values
			parts := []string{query.kind}
			for _, value := range values {
				parts = append(parts, fmt.Sprint(value))
			}
			snapshot.canonical = append(snapshot.canonical, strings.Join(parts, "|"))
			if query.kind == "node" {
				for _, label := range repoDependencyIfaLabels(values[0]) {
					snapshot.nodeCounts[label]++
				}
				continue
			}
			relationshipType := fmt.Sprint(values[2])
			snapshot.relationshipCounts[relationshipType]++
			if relationshipType == string(relationships.RelProvisionsDependencyFor) {
				snapshot.typedEdges = append(snapshot.typedEdges, repoDependencyIfaEdgeKey(
					repoDependencyIfaProperty(values[1], "id"),
					relationshipType,
					repoDependencyIfaProperty(values[5], "id"),
				))
			}
		}
		if err := result.Err(); err != nil {
			_ = session.Close(ctx)
			t.Fatalf("iterate snapshot query %d: %v", queryIndex, err)
		}
		if _, err := result.Consume(ctx); err != nil {
			_ = session.Close(ctx)
			t.Fatalf("consume snapshot query %d: %v", queryIndex, err)
		}
		if err := session.Close(ctx); err != nil {
			t.Fatalf("close snapshot query %d: %v", queryIndex, err)
		}
	}
	sort.Strings(snapshot.canonical)
	sort.Strings(snapshot.typedEdges)
	return snapshot
}

func repoDependencyIfaNodeSnapshotQueries(artifactIDs []string) []struct {
	kind string
	stmt cypher.Statement
} {
	query := func(cypherText string, parameters map[string]any) struct {
		kind string
		stmt cypher.Statement
	} {
		return struct {
			kind string
			stmt cypher.Statement
		}{kind: "node", stmt: cypher.Statement{Cypher: cypherText, Parameters: parameters}}
	}
	returnColumns := `RETURN labels(node), properties(node)`
	queries := []struct {
		kind string
		stmt cypher.Statement
	}{
		query(
			`MATCH (node:Repository {marker: $marker}) `+returnColumns,
			map[string]any{"marker": repoDependencyConcurrencyMarker},
		),
		query(
			`MATCH (node:Environment {name: $environment}) `+returnColumns,
			map[string]any{"environment": "ifa-prod-proof"},
		),
	}
	for _, artifactID := range artifactIDs {
		queries = append(queries, query(
			`MATCH (node:EvidenceArtifact {id: $artifact_id}) `+returnColumns,
			map[string]any{"artifact_id": artifactID},
		))
	}
	return queries
}

func repoDependencyIfaRelationshipSnapshotQueries(artifactIDs []string) []struct {
	kind string
	stmt cypher.Statement
} {
	query := func(cypherText string, parameters map[string]any) struct {
		kind string
		stmt cypher.Statement
	} {
		return struct {
			kind string
			stmt cypher.Statement
		}{
			kind: "relationship",
			stmt: cypher.Statement{Cypher: cypherText, Parameters: parameters},
		}
	}
	returnColumns := `RETURN labels(source), properties(source), type(rel), properties(rel),
		labels(target), properties(target)`
	parameters := repoDependencyIfaSnapshotParameters()
	queries := []struct {
		kind string
		stmt cypher.Statement
	}{
		query(
			`MATCH (source:Repository {marker: $marker})-[rel]->(target) `+returnColumns,
			parameters,
		),
		query(
			`MATCH (source:Environment {name: $environment})-[rel]->(target) `+returnColumns,
			parameters,
		),
	}
	for _, artifactID := range artifactIDs {
		artifactParameters := map[string]any{"artifact_id": artifactID}
		artifactPattern := `{id: $artifact_id}`
		queries = append(
			queries,
			query(
				`MATCH (source:EvidenceArtifact `+artifactPattern+`)-[rel]->(target) `+returnColumns,
				artifactParameters,
			),
		)
	}
	return queries
}

func repoDependencyIfaSnapshotParameters() map[string]any {
	return map[string]any{
		"marker":      repoDependencyConcurrencyMarker,
		"environment": "ifa-prod-proof",
	}
}

func repoDependencyIfaLabels(value any) []string {
	switch labels := value.(type) {
	case []string:
		return labels
	case []any:
		result := make([]string, 0, len(labels))
		for _, label := range labels {
			result = append(result, fmt.Sprint(label))
		}
		return result
	default:
		return nil
	}
}

func repoDependencyIfaProperty(value any, key string) string {
	properties, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(properties[key]))
}

func repoDependencyIfaEdgeKey(source, relationshipType, target string) string {
	return source + "\x00" + relationshipType + "\x00" + target
}

func assertRepoDependencyIfaCleanup(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	artifactIDs []string,
) {
	t.Helper()
	started := time.Now()
	deadline := started.Add(2 * time.Second)
	consecutiveCleanReads := 0
	for attempt := 1; ; attempt++ {
		residuals := repoDependencyIfaCleanupResiduals(ctx, t, exec, artifactIDs)
		if len(residuals) == 0 {
			consecutiveCleanReads++
			if consecutiveCleanReads == 2 {
				t.Logf("repo-dependency fixture cleanup visible after %d reads in %s", attempt, time.Since(started))
				return
			}
		} else {
			consecutiveCleanReads = 0
		}
		if time.Now().After(deadline) {
			t.Fatalf("repo-dependency fixture cleanup not visible after %s: %s", time.Since(started), strings.Join(residuals, ", "))
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for repo-dependency fixture cleanup visibility: %v", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func repoDependencyIfaCleanupResiduals(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	artifactIDs []string,
) []string {
	t.Helper()
	var residuals []string
	snapshot, err := readRepoDependencyIfaIdentitySnapshot(ctx, exec, artifactIDs)
	if err != nil {
		t.Fatalf("read cleanup identity snapshot: %v", err)
	}
	for _, repositoryID := range snapshot.repositoryIDs {
		if repositoryID != repoDependencyIfaExclusiveLockID {
			residuals = append(residuals, fmt.Sprintf("repository %q=1", repositoryID))
		}
	}
	for _, environmentID := range snapshot.environmentIDs {
		residuals = append(residuals, fmt.Sprintf("environment %q=1", environmentID))
	}
	artifactCounts := make(map[string]int64, len(snapshot.artifactIDs))
	for _, artifactID := range snapshot.artifactIDs {
		artifactCounts[artifactID]++
	}
	for _, artifactID := range artifactIDs {
		count := artifactCounts[artifactID]
		if count != 0 {
			residuals = append(residuals, fmt.Sprintf("artifact %q=%d", artifactID, count))
		}
	}
	return residuals
}

func assertRepoDependencyIfaSnapshot(
	t *testing.T,
	snapshot repoDependencyIfaSnapshot,
	expectedEdges []string,
) {
	t.Helper()
	if err := validateRepoDependencyIfaSnapshot(snapshot, expectedEdges); err != nil {
		t.Fatal(err)
	}
}

func validateRepoDependencyIfaSnapshot(
	snapshot repoDependencyIfaSnapshot,
	expectedEdges []string,
) error {
	wantEdges := append([]string(nil), expectedEdges...)
	sort.Strings(wantEdges)
	gotEdges := append([]string(nil), snapshot.typedEdges...)
	sort.Strings(gotEdges)
	if !slices.Equal(gotEdges, wantEdges) {
		return fmt.Errorf(
			"typed source/target edges=%v, want %v; relationship counts=%v canonical_rows=%d",
			gotEdges,
			wantEdges,
			snapshot.relationshipCounts,
			len(snapshot.canonical),
		)
	}
	wantNodes := map[string]int{
		"Repository":       11,
		"EvidenceArtifact": len(expectedEdges),
		"Environment":      1,
	}
	if !reflect.DeepEqual(snapshot.nodeCounts, wantNodes) {
		return fmt.Errorf(
			"fixture node counts=%v, want %v; canonical=%v",
			snapshot.nodeCounts,
			wantNodes,
			snapshot.canonical,
		)
	}
	wantRelationships := map[string]int{
		string(relationships.RelProvisionsDependencyFor): len(expectedEdges),
		"HAS_DEPLOYMENT_EVIDENCE":                        len(expectedEdges),
		"EVIDENCES_REPOSITORY_RELATIONSHIP":              len(expectedEdges),
		"TARGETS_ENVIRONMENT":                            len(expectedEdges),
	}
	if !reflect.DeepEqual(snapshot.relationshipCounts, wantRelationships) {
		return fmt.Errorf(
			"fixture relationship counts=%v, want %v; canonical=%v",
			snapshot.relationshipCounts,
			wantRelationships,
			snapshot.canonical,
		)
	}
	return nil
}
