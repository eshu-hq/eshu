// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live answer-truth proof for #6689 rows A3, A4, A9 and A10 on the pinned
// NornicDB image.
//
// A3 and A4 are the repo-context platform and dependency counts (the graph
// fallback used when the Postgres read model is unavailable), A9 is the catalog
// instance-environment enrichment, and A10 is the graph coverage fallback used
// when Postgres content coverage is unavailable. Each returned a wrong answer
// with no error on an older NornicDB build. The tests call the production
// functions against a seed whose right answer is known by construction.
//
// Run against an isolated container on the pinned image:
//
//	docker run -d --name eshu-answer-truth -e NORNICDB_NO_AUTH=true \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -p 127.0.0.1:27687:7687 \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-499-6ac958a9@sha256:fc90a2c3115d5dc0fe9a69ac676e5c77428bcfdcadc3320f2e99f887bea22f26
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27687 go test ./internal/query/repository \
//	  -tags live_nornicdb_answer_truth -run TestLiveNornicDBRepositoryAnswerTruth -count=1 -v
package repository

import (
	"context"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// repoAnswerTruthSeed uses literal property values only: on the pinned v1.3.3
// image an expression inside a CREATE property map can be stored as mangled
// literal text, so a computed id would never match the reads.
//
// Ground truth, by construction:
//   - repo A defines one workload with three instances (prod, staging, prod),
//     all on one platform: platform_count 1, instance_count 3, environments
//     [prod staging].
//   - repo A reaches repo B through DEPENDS_ON and USES_MODULE:
//     dependency_count 1.
//   - repo B contains two files holding three entities: file_count 2,
//     entity_count 3.
var repoAnswerTruthSeed = []string{
	`CREATE (:Repository {id: 'answer-truth-repo:repo-a', name: 'answer-truth-repo-a'})`,
	`CREATE (:Repository {id: 'answer-truth-repo:repo-b', name: 'answer-truth-repo-b'})`,
	`CREATE (:Workload {id: 'answer-truth-repo:wl-a', name: 'answer-truth-wl-a'})`,
	`CREATE (:WorkloadInstance {id: 'answer-truth-repo:wi-1', environment: 'prod'})`,
	`CREATE (:WorkloadInstance {id: 'answer-truth-repo:wi-2', environment: 'staging'})`,
	`CREATE (:WorkloadInstance {id: 'answer-truth-repo:wi-3', environment: 'prod'})`,
	`CREATE (:Platform {id: 'answer-truth-repo:plat-1', name: 'answer-truth-eks'})`,
	`CREATE (:File {id: 'answer-truth-repo:file-1', relative_path: 'a.go'})`,
	`CREATE (:File {id: 'answer-truth-repo:file-2', relative_path: 'b.go'})`,
	`CREATE (:Function {id: 'answer-truth-repo:fn-1', name: 'One'})`,
	`CREATE (:Function {id: 'answer-truth-repo:fn-2', name: 'Two'})`,
	`CREATE (:Function {id: 'answer-truth-repo:fn-3', name: 'Three'})`,
	repoAnswerTruthEdge("Repository", "answer-truth-repo:repo-a", "DEFINES", "Workload", "answer-truth-repo:wl-a"),
	repoAnswerTruthEdge("WorkloadInstance", "answer-truth-repo:wi-1", "INSTANCE_OF", "Workload", "answer-truth-repo:wl-a"),
	repoAnswerTruthEdge("WorkloadInstance", "answer-truth-repo:wi-2", "INSTANCE_OF", "Workload", "answer-truth-repo:wl-a"),
	repoAnswerTruthEdge("WorkloadInstance", "answer-truth-repo:wi-3", "INSTANCE_OF", "Workload", "answer-truth-repo:wl-a"),
	repoAnswerTruthEdge("WorkloadInstance", "answer-truth-repo:wi-1", "RUNS_ON", "Platform", "answer-truth-repo:plat-1"),
	repoAnswerTruthEdge("WorkloadInstance", "answer-truth-repo:wi-2", "RUNS_ON", "Platform", "answer-truth-repo:plat-1"),
	repoAnswerTruthEdge("WorkloadInstance", "answer-truth-repo:wi-3", "RUNS_ON", "Platform", "answer-truth-repo:plat-1"),
	repoAnswerTruthEdge("Repository", "answer-truth-repo:repo-a", "DEPENDS_ON", "Repository", "answer-truth-repo:repo-b"),
	repoAnswerTruthEdge("Repository", "answer-truth-repo:repo-a", "USES_MODULE", "Repository", "answer-truth-repo:repo-b"),
	repoAnswerTruthEdge("Repository", "answer-truth-repo:repo-b", "REPO_CONTAINS", "File", "answer-truth-repo:file-1"),
	repoAnswerTruthEdge("Repository", "answer-truth-repo:repo-b", "REPO_CONTAINS", "File", "answer-truth-repo:file-2"),
	repoAnswerTruthEdge("File", "answer-truth-repo:file-1", "CONTAINS", "Function", "answer-truth-repo:fn-1"),
	repoAnswerTruthEdge("File", "answer-truth-repo:file-2", "CONTAINS", "Function", "answer-truth-repo:fn-2"),
	repoAnswerTruthEdge("File", "answer-truth-repo:file-2", "CONTAINS", "Function", "answer-truth-repo:fn-3"),
}

func repoAnswerTruthEdge(fromLabel, fromID, relType, toLabel, toID string) string {
	return `MATCH (a:` + fromLabel + ` {id: '` + fromID + `'}) MATCH (b:` + toLabel + ` {id: '` + toID + `'}) CREATE (a)-[:` + relType + `]->(b)`
}

const repoAnswerTruthCleanup = `MATCH (n) WHERE n.id STARTS WITH 'answer-truth-repo:' DETACH DELETE n`

func TestLiveNornicDBRepositoryAnswerTruth(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	reader := repoLiveReader{driver: driver}
	reader.write(ctx, t, repoAnswerTruthCleanup)
	for _, stmt := range repoAnswerTruthSeed {
		reader.write(ctx, t, stmt)
	}
	defer reader.write(context.Background(), t, repoAnswerTruthCleanup)

	params := map[string]any{"repo_id": "answer-truth-repo:repo-a"}
	// A -1 fallback makes a statement that returns no row fail loudly instead
	// of reading as a plausible zero.
	fallback := map[string]any{"platform_count": -1, "dependency_count": -1}

	t.Run("A3 repo-context platform_count", func(t *testing.T) {
		got, err := queryRepositoryPlatformCount(ctx, reader, params, fallback, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("platform_count = %d", got)
		if got != 1 {
			t.Fatalf("platform_count = %d, want 1", got)
		}
	})

	t.Run("A4 repo-context dependency_count", func(t *testing.T) {
		got, err := queryRepositoryDependencyCount(ctx, reader, params, fallback, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("dependency_count = %d", got)
		if got != 1 {
			t.Fatalf("dependency_count = %d, want 1", got)
		}
	})

	t.Run("A9 catalog instance environments", func(t *testing.T) {
		handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
		enrichments, err := handler.catalogWorkloadEnrichments(ctx, []string{"answer-truth-repo:wl-a"})
		if err != nil {
			t.Fatal(err)
		}
		entry, ok := enrichments["answer-truth-repo:wl-a"]
		if !ok {
			t.Fatalf("no enrichment for the workload: %v", enrichments)
		}
		envs := append([]string(nil), entry.instanceEnvs...)
		sort.Strings(envs)
		t.Logf("repo_id=%q instance_count=%d environments=%v", entry.repoID, entry.instanceCount, envs)
		if entry.instanceCount != 3 {
			t.Fatalf("instance_count = %d, want 3", entry.instanceCount)
		}
		if want := []string{"prod", "staging"}; !reflect.DeepEqual(envs, want) {
			t.Fatalf("environments = %v, want %v", envs, want)
		}
		if entry.repoID != "answer-truth-repo:repo-a" {
			t.Fatalf("repo_id = %q, want answer-truth-repo:repo-a", entry.repoID)
		}
	})

	t.Run("A10 graph coverage fallback", func(t *testing.T) {
		handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
		stats, err := handler.queryRepositoryGraphCoverageStats(ctx, "answer-truth-repo:repo-b")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("coverage stats = %+v", stats)
		want := repositoryGraphCoverageStats{FileCount: 2, EntityCount: 3, Available: true}
		if stats != want {
			t.Fatalf("coverage stats = %+v, want %+v", stats, want)
		}
	})
}

// repoLiveReader is the test-only live GraphQuery for this file. The package
// cannot import root query's Neo4jReader without a cycle.
type repoLiveReader struct {
	driver neo4jdriver.DriverWithContext
}

func (r repoLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r repoLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (r repoLiveReader) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume %q: %v", cypher, err)
	}
}
