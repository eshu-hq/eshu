// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for the #7245 PR-2 readiness probe on the pinned Neo4j: a node a
// posture or base_refs writer creates with no searched field does not hold
// readiness off, an unkeyed node with any one of the twelve searched fields
// does, and an overflow-marked node counts as keyed. Run it like
// differential_live_test.go.
package fulltext_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

func TestLiveInfraSearchReadinessIgnoresNodesWithNoSearchedField(t *testing.T) {
	g := openLiveFulltextGraph(t)
	token := fmt.Sprintf("zz7245p%d", time.Now().UnixNano()%1_000_000_000)
	t.Cleanup(func() {
		g.mustWrite(t, "MATCH (n) WHERE n.uid STARTS WITH $t DETACH DELETE n", map[string]any{"t": token})
	})
	g.keyAll(t)
	writer := liveFulltextWriter{g: g}
	total := func(count func(context.Context, search.Runner) (map[string]int64, error)) int64 {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		counts, err := count(ctx, writer)
		if err != nil {
			t.Fatal(err)
		}
		var n int64
		for _, c := range counts {
			n += c
		}
		return n
	}
	baseSearchable, baseAll := total(search.CountUnkeyedSearchable), total(search.CountUnkeyed)

	// The posture writers and the KustomizeOverlay base_refs write MERGE by
	// uid and set only their own properties.
	g.mustWrite(t, "MERGE (r:CloudResource {uid: $t + ':bare'}) SET r.ec2_internet_exposure_state = 'exposed', r.rds_identifier = 'db-1'", map[string]any{"t": token})
	g.mustWrite(t, "MERGE (k:KustomizeOverlay {uid: $t + ':bare-overlay'}) SET k.base_refs = ['base']", map[string]any{"t": token})
	// An overflow-marked node carries the flag and no key, as the writer
	// clause leaves it.
	g.mustWrite(t, "CREATE (:TerraformResource {uid: $t + ':ovf', name: $t + '-overflowed', infra_search_ovf: 'T'})", map[string]any{"t": token})
	if got := total(search.CountUnkeyedSearchable); got != baseSearchable {
		t.Fatalf("searchable unkeyed = %d after bare and overflow nodes, want %d", got, baseSearchable)
	}
	if got := total(search.CountUnkeyed); got != baseAll+2 {
		t.Fatalf("CountUnkeyed = %d, want %d: it still counts every node without a flag", got, baseAll+2)
	}
	readiness := fulltext.NewReadiness(g, nil, nil)
	st := readiness.Refresh(context.Background())
	if st.Unkeyed != baseSearchable || st.Err != nil {
		t.Fatalf("readiness with bare nodes = %+v, want unkeyed %d", st, baseSearchable)
	}
	// On a store the backfill keyed fully, the bare nodes leave it ready.
	if baseSearchable == 0 && (!st.Ready || st.Reason != fulltext.ReasonReady) {
		t.Fatalf("readiness with only bare and overflow nodes unkeyed = %+v, want ready", st)
	}

	// Each searched field alone makes an unkeyed node count, string or not.
	for _, field := range search.Fields() {
		for _, value := range []any{"v-" + token, int64(7)} {
			uid := token + ":field:" + field
			g.mustWrite(t, "CREATE (n:CloudResource {uid: $uid}) SET n."+field+" = $v", map[string]any{"uid": uid, "v": value})
			if got := total(search.CountUnkeyedSearchable); got != baseSearchable+1 {
				t.Fatalf("field %s = %v: searchable unkeyed = %d, want %d", field, value, got, baseSearchable+1)
			}
			st := readiness.Refresh(context.Background())
			if st.Ready || st.Reason != fulltext.ReasonUnkeyed {
				t.Fatalf("field %s = %v: readiness = %+v, want not ready (unkeyed)", field, value, st)
			}
			g.mustWrite(t, "MATCH (n:CloudResource {uid: $uid}) DETACH DELETE n", map[string]any{"uid": uid})
		}
	}
}
