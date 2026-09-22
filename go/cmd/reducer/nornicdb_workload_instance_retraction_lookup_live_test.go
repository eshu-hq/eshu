// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live answer-truth proof for issue #6786 shape X9: the trigger is an UNWIND
// variable name that equals a RETURN alias, with a MATCH in between.
// neo4jWorkloadInstanceRetractionLookup.ListWorkloadInstances ran
//
//	UNWIND $repo_ids AS repo_id
//	MATCH (i:WorkloadInstance {repo_id: repo_id})
//	WHERE i.evidence_source = $evidence_source
//	RETURN DISTINCT i.repo_id AS repo_id, i.id AS instance_id
//
// On NornicDB v1.3.3 the first RETURN column comes back named after the
// FIRST UNWIND value (e.g. the literal key "'r1'") instead of "repo_id", so
// query.StringVal(row, "repo_id") reads "" and ListWorkloadInstances' own
// empty-field guard drops every row. The reducer's instance-retraction pass
// (WorkloadMaterializationHandler.Handle ->
// reducer.ReconcileWorkloadInstanceRetraction ->
// lookup.ListWorkloadInstances, internal/reducer/workload_materialization_handler.go
// and internal/reducer/workload_instance_retraction.go) then sees zero
// existing instances for every repository in the current materialization
// pass and never retracts a stale WorkloadInstance node superseded by
// #5473's environment-alias canonicalization -- the exact node
// WorkloadMaterializer.RetractInstances (proven live by
// TestBoltWorkloadInstanceRetractRespectsDeleteTimeOwnershipPredicate in
// workload_instance_retraction_live_test.go, which drives the DELETE side
// only and never exercises this lookup) is supposed to delete.
//
// Neo4j evaluates the same statement correctly (the RETURN alias always
// wins there), so this is NornicDB-only.
//
// Run against isolated containers on the pinned images:
//
//	docker run -d --name eshu-6786-fix2-nornic -p 127.0.0.1:27900:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_ASYNC_WRITES_ENABLED=false \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -e NORNICDB_HEIMDALL_ENABLED=false \
//	  -e NORNICDB_SEARCH_BM25_ENABLED=false -e NORNICDB_SEARCH_VECTOR_ENABLED=false \
//	  -e NORNICDB_QDRANT_GRPC_ENABLED=false -e NORNICDB_PERSIST_SEARCH_INDEXES=false \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-499-6ac958a9@sha256:fc90a2c3115d5dc0fe9a69ac676e5c77428bcfdcadc3320f2e99f887bea22f26
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27900 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./cmd/reducer -tags live_nornicdb_answer_truth \
//	  -run TestLiveWorkloadInstanceRetractionLookupAnswerTruth -count=1 -v
//
//	docker run -d --name eshu-6786-fix2-neo4j -p 127.0.0.1:27910:7687 \
//	  -e NEO4J_AUTH=none neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27910 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./cmd/reducer -tags live_nornicdb_answer_truth \
//	  -run TestLiveWorkloadInstanceRetractionLookupAnswerTruth -count=1 -v
package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	eshugraph "github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	x9RepoOwned   = "repository:6786-x9-owned"
	x9RepoOther   = "repository:6786-x9-other"
	x9InstanceID  = "workload-instance:6786-x9:prod"
	x9OtherSource = "operator/declared"
)

var x9Seed = []string{
	`CREATE (:Repository {id: '` + x9RepoOwned + `'})`,
	`CREATE (:Repository {id: '` + x9RepoOther + `'})`,
	`CREATE (:WorkloadInstance {id: '` + x9InstanceID + `', repo_id: '` + x9RepoOwned + `', evidence_source: '` + reducer.EvidenceSourceWorkloads + `'})`,
}

const x9Cleanup = `MATCH (n) WHERE n.id STARTS WITH 'repository:6786-x9-' OR n.id STARTS WITH 'workload-instance:6786-x9' DETACH DELETE n`

func TestLiveWorkloadInstanceRetractionLookupAnswerTruth(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend := strings.TrimSpace(strings.ToLower(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	if backend == "" {
		t.Fatal("ESHU_LIVE_GRAPH_BACKEND is required (nornicdb|neo4j)")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		if backend == "nornicdb" {
			database = "nornic"
		} else {
			database = "neo4j"
		}
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

	writer := x9LiveWriter{driver: driver, database: database}
	writer.write(ctx, t, x9Cleanup)
	defer writer.write(context.Background(), t, x9Cleanup)

	schemaBackend := eshugraph.SchemaBackendNeo4j
	if backend == "nornicdb" {
		schemaBackend = eshugraph.SchemaBackendNornicDB
	}
	if err := eshugraph.EnsureSchemaWithBackend(ctx, writer, nil, schemaBackend); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	for _, stmt := range x9Seed {
		writer.write(ctx, t, stmt)
	}

	// The production reader (query.NewNeo4jReader), the same one
	// cmd/reducer's wiring passes to neo4jWorkloadInstanceRetractionLookup
	// (see main.go's InstanceRetractionLookup wiring) -- no test-only
	// reimplementation of the read path.
	reader := query.NewNeo4jReader(driver, database)
	lookup := neo4jWorkloadInstanceRetractionLookup{reader: reader}

	for _, tc := range []struct {
		name           string
		repoIDs        []string
		evidenceSource string
		wantInstances  []reducer.ExistingWorkloadInstance
	}{
		{
			name:           "owning repo and matching evidence source returns the instance",
			repoIDs:        []string{x9RepoOwned},
			evidenceSource: reducer.EvidenceSourceWorkloads,
			wantInstances: []reducer.ExistingWorkloadInstance{
				{RepoID: x9RepoOwned, InstanceID: x9InstanceID},
			},
		},
		{
			name:           "unrelated repo returns nothing",
			repoIDs:        []string{x9RepoOther},
			evidenceSource: reducer.EvidenceSourceWorkloads,
			wantInstances:  nil,
		},
		{
			name:           "wrong evidence source returns nothing",
			repoIDs:        []string{x9RepoOwned},
			evidenceSource: x9OtherSource,
			wantInstances:  nil,
		},
		{
			name:           "multiple repo ids still resolve the owning one (UNWIND with more than one value)",
			repoIDs:        []string{x9RepoOther, x9RepoOwned},
			evidenceSource: reducer.EvidenceSourceWorkloads,
			wantInstances: []reducer.ExistingWorkloadInstance{
				{RepoID: x9RepoOwned, InstanceID: x9InstanceID},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := lookup.ListWorkloadInstances(ctx, tc.repoIDs, tc.evidenceSource)
			if err != nil {
				t.Fatalf("ListWorkloadInstances() error = %v", err)
			}
			if len(got) != len(tc.wantInstances) {
				t.Fatalf("ListWorkloadInstances() = %#v, want %#v", got, tc.wantInstances)
			}
			for i, want := range tc.wantInstances {
				if got[i] != want {
					t.Errorf("ListWorkloadInstances()[%d] = %#v, want %#v", i, got[i], want)
				}
			}
		})
	}
}

// x9LiveWriter is the test-only live graph.CypherExecutor for seeding and
// schema application in this file. It cannot reuse repositoryLiveReader /
// codequeryLiveReader (different packages, same rationale as those types'
// own doc comments: no shared cross-package test helper without a cycle).
type x9LiveWriter struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

// ExecuteCypher implements graph.CypherExecutor so this writer can drive
// graph.EnsureSchemaWithBackend.
func (w x9LiveWriter) ExecuteCypher(ctx context.Context, stmt eshugraph.CypherStatement) error {
	return x9RunWriteWithRetry(ctx, func(ctx context.Context) error {
		session := w.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: w.database})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
		if err != nil {
			return err
		}
		_, err = result.Consume(ctx)
		return err
	})
}

func (w x9LiveWriter) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	err := x9RunWriteWithRetry(ctx, func(ctx context.Context) error {
		session := w.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: w.database})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, cypher, nil)
		if err != nil {
			return err
		}
		_, err = result.Consume(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
}

// x9WriteMaxAttempts bounds the retry count for a transient graph-write
// conflict (see x9IsTransientError). Go runs different test packages
// concurrently by default even with no t.Parallel(), so this live test can
// race a sibling package's live test against the same shared NornicDB/Neo4j
// instance in CI (#6784).
const x9WriteMaxAttempts = 5

func x9RetryDelay(attempt int) time.Duration {
	return time.Duration(attempt) * 100 * time.Millisecond
}

// x9IsTransientError reports whether err is a transient, safe-to-retry write
// conflict -- observed live as "Neo.TransientError.Transaction.Outdated ...
// Please retry" when two test packages' live writes race the same shared
// NornicDB/Neo4j instance.
func x9IsTransientError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "TransientError")
}

func x9RunWriteWithRetry(ctx context.Context, run func(context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= x9WriteMaxAttempts; attempt++ {
		lastErr = run(ctx)
		if lastErr == nil {
			return nil
		}
		if !x9IsTransientError(lastErr) || attempt == x9WriteMaxAttempts {
			return lastErr
		}
		time.Sleep(x9RetryDelay(attempt))
	}
	return lastErr
}
