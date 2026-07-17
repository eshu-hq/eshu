// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveInfraProviderInventoryBucketsNonNull is the backend-required proof for
// #5283. On the pinned NornicDB build the all-categories by-provider grouping
// expression previously used a nested `CASE WHEN n:CloudResource THEN ...` label
// test inside a CASE; NornicDB echoes a label test in a projection as the literal
// expression text, so the enclosing CASE collapsed to a null bucket and even
// nodes with a real provider mis-bucketed. This test seeds provider-less and
// provider-bearing nodes across a CloudResource and a non-cloud label, runs the
// shipped GraphInfraResourceAggregateStore.InfraResourceInventory by-provider
// read, and asserts the corrected buckets: no empty ("") bucket, the
// CloudResource source_system fallback is preserved, and a real provider is not
// collapsed to unknown.
//
//	Run: ESHU_INFRA_AGG_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 \
//		go test ./internal/query -run TestLiveInfraProviderInventoryBucketsNonNull -count=1 -v
func TestLiveInfraProviderInventoryBucketsNonNull(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_INFRA_AGG_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_INFRA_AGG_PROVE_LIVE=1 to run the live infra provider-inventory proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required (e.g. bolt://localhost:17687)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	write := func(cypher string, params map[string]any) {
		s := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
		defer func() { _ = s.Close(ctx) }()
		if _, err := s.Run(ctx, cypher, params); err != nil {
			t.Fatalf("seed write failed: %v\ncypher=%s", err, cypher)
		}
	}

	// Unique synthetic ids so cleanup deletes exactly this test's nodes.
	ids := []string{
		"infra5283:cr-aws",   // CloudResource, provider-less, source_system present -> "aws"
		"infra5283:cr-empty", // CloudResource, provider-less, source_system empty   -> "unknown"
		"infra5283:tf-none",  // TerraformResource, provider-less                    -> "unknown"
		"infra5283:tf-gcp",   // TerraformResource, provider present                 -> "gcp"
	}
	cleanup := func() {
		for _, id := range ids {
			write(`MATCH (n {id:$id}) DETACH DELETE n`, map[string]any{"id": id})
		}
	}
	cleanup()
	defer cleanup()
	write(`CREATE (n:CloudResource {id:$id, provider:'', source_system:'aws'})`, map[string]any{"id": "infra5283:cr-aws"})
	write(`CREATE (n:CloudResource {id:$id, provider:'', source_system:''})`, map[string]any{"id": "infra5283:cr-empty"})
	write(`CREATE (n:TerraformResource {id:$id, provider:''})`, map[string]any{"id": "infra5283:tf-none"})
	write(`CREATE (n:TerraformResource {id:$id, provider:'gcp'})`, map[string]any{"id": "infra5283:tf-gcp"})

	store := NewGraphInfraResourceAggregateStore(NewNeo4jReader(driver, "nornic"))

	// assertProviderBuckets checks that no empty/null bucket is returned and that
	// this test's seeded contribution buckets correctly. A shared backend may
	// hold other nodes, so require at-least counts rather than whole-graph
	// equality.
	assertProviderBuckets := func(label string, got map[string]int) {
		if _, hasNull := got[""]; hasNull {
			t.Errorf("%s: provider grouping returned an empty/null bucket (count=%d); provider-less resources must bucket as \"unknown\" (#5283)", label, got[""])
		}
		for bucket, want := range map[string]int{"aws": 1, "gcp": 1, "unknown": 2} {
			if got[bucket] < want {
				t.Errorf("%s: provider bucket %q = %d, want >= %d (seeded contribution)", label, bucket, got[bucket], want)
			}
		}
		if t.Failed() {
			t.Logf("%s buckets: %#v", label, got)
		}
	}

	// InfraResourceInventory by-provider (the /infra/resources/inventory dimension).
	rows, err := store.InfraResourceInventory(ctx, InfraResourceAggregateFilter{}, InfraResourceInventoryByProvider, 100, 0)
	if err != nil {
		t.Fatalf("InfraResourceInventory by-provider: %v", err)
	}
	inventory := map[string]int{}
	for _, row := range rows {
		inventory[strings.TrimSpace(row.Value)] += row.Count
	}
	assertProviderBuckets("inventory", inventory)

	// CountInfraResources.ByProvider (the /infra/resources/count rollup named in
	// the issue). It reuses infraResourceProviderGroupExpression via the same
	// per-label CALL builder, so it must show the same corrected buckets.
	count, err := store.CountInfraResources(ctx, InfraResourceAggregateFilter{})
	if err != nil {
		t.Fatalf("CountInfraResources: %v", err)
	}
	assertProviderBuckets("count-by-provider", count.ByProvider)
}
