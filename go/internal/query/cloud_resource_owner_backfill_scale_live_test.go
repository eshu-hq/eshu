// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

const (
	cloudResourceOwnerBackfillScaleLiveEnv    = "ESHU_CLOUD_RESOURCE_BACKFILL_NORNICDB_SCALE_LIVE"
	cloudResourceOwnerBackfillScaleNodesEnv   = "ESHU_CLOUD_RESOURCE_BACKFILL_NORNICDB_SCALE_NODES"
	cloudResourceOwnerBackfillScaleCleanupEnv = "ESHU_CLOUD_RESOURCE_BACKFILL_NORNICDB_SCALE_CLEANUP"
	// cloudResourceOwnerBackfillScaleDefaultNodes matches the population #6842
	// measured: 150,000 uid-constrained CloudResource nodes, where the first
	// page shipped without a uid range predicate failed the 10s graph-read
	// deadline (raw shape 29.5s to 45.9s with a 150s cap).
	cloudResourceOwnerBackfillScaleDefaultNodes = 150_000
	cloudResourceOwnerBackfillScalePrefix       = "cloud-owner-backfill-scale-"
	// cloudResourceOwnerBackfillScaleSeedBatch matches the constrained-label
	// batch size the read-API latency gate uses (iacGraphSeedBatchSize): a uid
	// UNIQUE constraint makes per-row write cost grow with batch size.
	cloudResourceOwnerBackfillScaleSeedBatch   = 250
	cloudResourceOwnerBackfillScaleDeleteBatch = 1_000
	// cloudResourceOwnerBackfillScaleUIDLookupIndex is the NornicDB uid lookup
	// index graph.nornicDBUIDLookupIndexes creates for CloudResource.
	cloudResourceOwnerBackfillScaleUIDLookupIndex = "nornicdb_cloud_resource_uid_lookup"
)

// TestCloudResourceOwnerBackfillerFirstPageMeetsDeadlineAtScale is the #6842
// regression: the startup owner-ledger backfill's first page must finish
// inside the production graph-read deadline on a CloudResource population
// large enough that an `ORDER BY n.uid LIMIT $limit` page with no uid range
// predicate does not. It drives the production Backfill entry point through
// the production Neo4jReader policy (10s deadline), so the shape under test is
// exactly what cmd/api and cmd/mcp-server run at boot.
//
// It applies Eshu's full NornicDB schema once, before seeding an empty
// store, as production bootstrap does. That matters: on NornicDB the uid
// uniqueness constraint creates no index (SHOW INDEXES stays empty), and it
// is the separate nornicdb_cloud_resource_uid_lookup index from that schema
// that the range predicate seeks; with only the constraint present every uid
// read, keyset page or point lookup alike, is a label scan.
//
// It is written for a disposable NornicDB container: the seed is reused when
// the graph already holds exactly the expected prefixed population, so the
// same seeded store can serve a before/after comparison, and it refuses a
// graph holding any other CloudResource node because the production backfill
// query is unscoped. On reuse the schema is NOT re-applied: on the pinned
// build `CREATE INDEX ... IF NOT EXISTS` for an existing index re-runs the
// index backfill and appends every node id a second time, after which every
// index-driven read returns each row twice. The reuse path instead checks
// that the uid lookup index is present. Cleanup runs only when
// ESHU_CLOUD_RESOURCE_BACKFILL_NORNICDB_SCALE_CLEANUP=1.
//
//	ESHU_CLOUD_RESOURCE_BACKFILL_NORNICDB_SCALE_LIVE=1 \
//	ESHU_NEO4J_URI=bolt://127.0.0.1:18697 \
//	go test ./internal/query -run TestCloudResourceOwnerBackfillerFirstPageMeetsDeadlineAtScale -count=1 -v -timeout 60m
func TestCloudResourceOwnerBackfillerFirstPageMeetsDeadlineAtScale(t *testing.T) {
	if strings.TrimSpace(os.Getenv(cloudResourceOwnerBackfillScaleLiveEnv)) == "" {
		t.Skip("set " + cloudResourceOwnerBackfillScaleLiveEnv + "=1 to run the live NornicDB scale proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	total := cloudResourceOwnerBackfillScaleDefaultNodes
	if raw := strings.TrimSpace(os.Getenv(cloudResourceOwnerBackfillScaleNodesEnv)); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < cloudResourceBackfillDefaultPageSize {
			t.Fatalf("%s=%q must be an integer >= %d", cloudResourceOwnerBackfillScaleNodesEnv, raw, cloudResourceBackfillDefaultPageSize)
		}
		total = parsed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open NornicDB driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	seed := cloudResourceBackfillScaleSeed{t: t, ctx: ctx, driver: driver, total: total}
	// Registered before seeding so a rerun with the cleanup flag also clears
	// a partial seed left by an earlier interrupted run.
	if strings.TrimSpace(os.Getenv(cloudResourceOwnerBackfillScaleCleanupEnv)) != "" {
		defer seed.cleanup()
	}
	seed.ensurePopulation()

	reader := NewNeo4jReader(driver, "nornic")
	graph := &firstPageOnlyCloudResourceBackfillGraph{GraphQuery: reader}
	store := &recordingCloudResourceBackfillStore{}
	err = (CloudResourceOwnerBackfiller{
		Graph:    graph,
		Store:    store,
		PageSize: cloudResourceBackfillDefaultPageSize,
	}).Backfill(ctx)
	if errors.Is(err, ErrGraphReadDeadline) {
		t.Fatalf("first backfill page exceeded the %s graph-read deadline on %d CloudResource nodes (elapsed %s): %v",
			defaultGraphReadTimeout, total, graph.firstPageElapsed, err)
	}
	if err != nil {
		t.Fatalf("Backfill() first page on %d CloudResource nodes: %v", total, err)
	}
	if got, want := len(store.seeded), cloudResourceBackfillDefaultPageSize; got != want {
		t.Fatalf("first page seeded %d rows, want %d", got, want)
	}
	for i, entry := range store.seeded {
		if want := cloudResourceBackfillScaleUID(i); entry.UID != want {
			t.Fatalf("first page uid[%d] = %q, want %q (page must be the lowest uids in order)", i, entry.UID, want)
		}
	}
	t.Logf("first backfill page of %d rows over %d CloudResource nodes returned in %s (deadline %s)",
		cloudResourceBackfillDefaultPageSize, total, graph.firstPageElapsed, defaultGraphReadTimeout)
}

func cloudResourceBackfillScaleUID(i int) string {
	return fmt.Sprintf("%s%07d", cloudResourceOwnerBackfillScalePrefix, i)
}

// firstPageOnlyCloudResourceBackfillGraph forwards the first backfill read to
// the live reader and answers every later page with no rows, so Backfill
// exercises exactly the production first-page shape and then stops.
type firstPageOnlyCloudResourceBackfillGraph struct {
	GraphQuery
	calls            int
	firstPageElapsed time.Duration
}

func (g *firstPageOnlyCloudResourceBackfillGraph) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.calls++
	if g.calls > 1 {
		return nil, nil
	}
	started := time.Now()
	rows, err := g.GraphQuery.Run(ctx, cypher, params)
	g.firstPageElapsed = time.Since(started)
	return rows, err
}

type cloudResourceBackfillScaleSeed struct {
	t      *testing.T
	ctx    context.Context
	driver neo4jdriver.DriverWithContext
	total  int
}

func (s cloudResourceBackfillScaleSeed) write(cypher string, params map[string]any) {
	s.t.Helper()
	session := s.driver.NewSession(s.ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: "nornic",
	})
	defer func() { _ = session.Close(s.ctx) }()
	result, err := session.Run(s.ctx, cypher, params)
	if err != nil {
		s.t.Fatalf("live NornicDB write: %v", err)
	}
	if _, err := result.Consume(s.ctx); err != nil {
		s.t.Fatalf("consume live NornicDB write: %v", err)
	}
}

func (s cloudResourceBackfillScaleSeed) count(cypher string, params map[string]any) int {
	s.t.Helper()
	row, err := NewNeo4jReader(s.driver, "nornic").RunSingle(s.ctx, cypher, params)
	if err != nil {
		s.t.Fatalf("count CloudResource nodes: %v", err)
	}
	return IntVal(row, "count")
}

// requireUIDLookupIndex fails unless the production uid lookup index exists,
// so a reused seed is measured on a deployment-shaped store without
// re-applying the schema (see the test comment for why re-applying is unsafe).
func (s cloudResourceBackfillScaleSeed) requireUIDLookupIndex() {
	s.t.Helper()
	rows, err := NewNeo4jReader(s.driver, "nornic").Run(s.ctx, "SHOW INDEXES", nil)
	if err != nil {
		s.t.Fatalf("SHOW INDEXES: %v", err)
	}
	for _, row := range rows {
		if StringVal(row, "name") == cloudResourceOwnerBackfillScaleUIDLookupIndex {
			return
		}
	}
	s.t.Fatalf("reused seed lacks the %s index; use a fresh container", cloudResourceOwnerBackfillScaleUIDLookupIndex)
}

// ensureSchema applies the production NornicDB schema (uid constraints plus
// the NornicDB uid lookup indexes) to an empty store so it is shaped like a
// deployment. It must run once, before seeding.
func (s cloudResourceBackfillScaleSeed) ensureSchema() {
	s.t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := graph.EnsureSchemaWithBackend(s.ctx, cloudResourceBackfillScaleExecutor{driver: s.driver}, logger, graph.SchemaBackendNornicDB); err != nil {
		s.t.Fatalf("apply NornicDB schema: %v", err)
	}
}

// cloudResourceBackfillScaleExecutor runs schema DDL for the scale proof.
type cloudResourceBackfillScaleExecutor struct {
	driver neo4jdriver.DriverWithContext
}

func (e cloudResourceBackfillScaleExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, e.driver, stmt.Cypher, stmt.Parameters,
		neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase("nornic"))
	return err
}

func (s cloudResourceBackfillScaleSeed) ensurePopulation() {
	all := s.count(`MATCH (n:CloudResource) RETURN count(n) AS count`, nil)
	mine := s.count(`MATCH (n:CloudResource) WHERE n.uid STARTS WITH $prefix RETURN count(n) AS count`,
		map[string]any{"prefix": cloudResourceOwnerBackfillScalePrefix})
	if all != mine {
		s.t.Fatalf("scale proof requires a disposable graph: %d CloudResource nodes present, %d carry the %q prefix",
			all, mine, cloudResourceOwnerBackfillScalePrefix)
	}
	switch {
	case mine == s.total:
		s.requireUIDLookupIndex()
		s.t.Logf("reusing %d seeded CloudResource nodes", mine)
		return
	case mine != 0:
		s.t.Fatalf("graph holds %d prefixed CloudResource nodes, want 0 or %d; rerun with %s=1 to clear them",
			mine, s.total, cloudResourceOwnerBackfillScaleCleanupEnv)
	}

	s.ensureSchema()
	started := time.Now()
	for start := 0; start < s.total; start += cloudResourceOwnerBackfillScaleSeedBatch {
		end := min(start+cloudResourceOwnerBackfillScaleSeedBatch, s.total)
		rows := make([]map[string]any, 0, end-start)
		for i := start; i < end; i++ {
			rows = append(rows, map[string]any{
				"uid":            cloudResourceBackfillScaleUID(i),
				"source_fact_id": fmt.Sprintf("fact-%07d", i),
				"name":           fmt.Sprintf("bucket-%07d", i),
			})
		}
		s.write(`
UNWIND $rows AS row
CREATE (n:CloudResource)
SET n.uid = row.uid,
    n.id = row.uid,
    n.resource_type = 'aws_s3_bucket',
    n.source_fact_id = row.source_fact_id,
    n.collector_kind = 'aws',
    n.name = row.name`, map[string]any{"rows": rows})
		if end%25_000 == 0 || end == s.total {
			s.t.Logf("seeded %d/%d CloudResource nodes in %s", end, s.total, time.Since(started).Round(time.Second))
		}
	}
	if got := s.count(`MATCH (n:CloudResource) WHERE n.uid STARTS WITH $prefix RETURN count(n) AS count`,
		map[string]any{"prefix": cloudResourceOwnerBackfillScalePrefix}); got != s.total {
		s.t.Fatalf("seeded CloudResource nodes = %d, want %d", got, s.total)
	}
}

func (s cloudResourceBackfillScaleSeed) cleanup() {
	started := time.Now()
	for start := 0; start < s.total; start += cloudResourceOwnerBackfillScaleDeleteBatch {
		end := min(start+cloudResourceOwnerBackfillScaleDeleteBatch, s.total)
		uids := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			uids = append(uids, cloudResourceBackfillScaleUID(i))
		}
		s.write(`UNWIND $uids AS uid MATCH (n:CloudResource {uid: uid}) DETACH DELETE n`, map[string]any{"uids": uids})
	}
	s.t.Logf("deleted %d seeded CloudResource nodes in %s", s.total, time.Since(started).Round(time.Second))
}
