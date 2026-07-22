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

const cloudResourceOwnerBackfillLiveEnv = "ESHU_CLOUD_RESOURCE_BACKFILL_NORNICDB_LIVE"

// TestCloudResourceOwnerBackfillerNornicDBLive proves the production first-page
// and continuation queries against an isolated NornicDB. It refuses to seed a
// graph that already contains CloudResource nodes so the unscoped production
// backfill cannot read or mutate retained evidence.
//
// Run against an isolated NornicDB:
//
//	ESHU_CLOUD_RESOURCE_BACKFILL_NORNICDB_LIVE=1 \
//	ESHU_NEO4J_URI=bolt://localhost:17687 \
//	go test ./internal/query -run TestCloudResourceOwnerBackfillerNornicDBLive -count=1 -v
func TestCloudResourceOwnerBackfillerNornicDBLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv(cloudResourceOwnerBackfillLiveEnv)) == "" {
		t.Skip("set " + cloudResourceOwnerBackfillLiveEnv + "=1 to run the live NornicDB backfill proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open NornicDB driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	reader := NewNeo4jReader(driver, "nornic")
	countRow, err := reader.RunSingle(ctx, `MATCH (n:CloudResource) RETURN count(n) AS count`, nil)
	if err != nil {
		t.Fatalf("count existing CloudResource nodes: %v", err)
	}
	if got := IntVal(countRow, "count"); got != 0 {
		t.Fatalf("live proof requires an isolated graph with zero CloudResource nodes, got %d", got)
	}

	const total = 1201
	prefix := fmt.Sprintf("cloud-owner-backfill-live-%d-", time.Now().UnixNano())
	rows := make([]map[string]any, 0, total)
	uids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		uid := fmt.Sprintf("%s%04d", prefix, i)
		uids = append(uids, uid)
		rows = append(rows, map[string]any{
			"uid":            uid,
			"source_fact_id": fmt.Sprintf("fact-%04d", i),
			"resource_type":  "aws_s3_bucket",
			"name":           fmt.Sprintf("bucket-%04d", i),
		})
	}

	write := func(cypher string, params map[string]any) {
		t.Helper()
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeWrite,
			DatabaseName: "nornic",
		})
		defer func() { _ = session.Close(ctx) }()
		result, runErr := session.Run(ctx, cypher, params)
		if runErr != nil {
			t.Fatalf("live NornicDB write: %v", runErr)
		}
		if _, consumeErr := result.Consume(ctx); consumeErr != nil {
			t.Fatalf("consume live NornicDB write: %v", consumeErr)
		}
	}
	cleanup := func() {
		for start := 0; start < len(uids); start += 200 {
			end := min(start+200, len(uids))
			write(`UNWIND $uids AS uid MATCH (n:CloudResource {uid: uid}) DETACH DELETE n`, map[string]any{
				"uids": uids[start:end],
			})
		}
	}
	defer cleanup()

	for start := 0; start < len(rows); start += 200 {
		end := min(start+200, len(rows))
		write(`
UNWIND $rows AS row
CREATE (n:CloudResource)
SET n.uid = row.uid,
    n.id = row.uid,
    n.resource_type = row.resource_type,
    n.source_fact_id = row.source_fact_id,
    n.collector_kind = 'aws',
    n.name = row.name`, map[string]any{"rows": rows[start:end]})
	}

	graph := &liveCloudResourceBackfillGraph{GraphQuery: reader}
	store := &recordingCloudResourceBackfillStore{}
	started := time.Now()
	if err := (CloudResourceOwnerBackfiller{
		Graph:    graph,
		Store:    store,
		PageSize: 500,
	}).Backfill(ctx); err != nil {
		t.Fatalf("Backfill() against live NornicDB: %v", err)
	}
	duration := time.Since(started)

	if !store.markedComplete {
		t.Fatal("live NornicDB backfill did not mark completion")
	}
	if got := len(store.seeded); got != total {
		t.Fatalf("seeded rows = %d, want %d", got, total)
	}
	if got, want := graph.afterUIDs, []string{"", uids[499], uids[999]}; !equalStrings(got, want) {
		t.Fatalf("page cursor sequence = %#v, want %#v", got, want)
	}
	for i, entry := range store.seeded {
		if entry.UID != uids[i] {
			t.Fatalf("seeded uid[%d] = %q, want %q", i, entry.UID, uids[i])
		}
	}
	t.Logf("backfilled %d CloudResource rows in 3 pages (500/500/201) in %s", total, duration)
}

type liveCloudResourceBackfillGraph struct {
	GraphQuery
	afterUIDs []string
}

func (g *liveCloudResourceBackfillGraph) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.afterUIDs = append(g.afterUIDs, StringVal(params, "after_uid"))
	return g.GraphQuery.Run(ctx, cypher, params)
}
