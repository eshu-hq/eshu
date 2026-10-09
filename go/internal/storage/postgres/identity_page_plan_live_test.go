// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// identityPagePlan is what the plan guard reads from one EXPLAIN (FORMAT JSON)
// of the identity page query.
type identityPagePlan struct {
	raw       string
	nodeTypes []string
	// identityIndexCond and identityIndexFilter are the Index Cond and Filter
	// text of the Index Scan node on fact_records_identity_epoch_idx_v2, the
	// node the page rides when the plan is healthy.
	identityIndexScan   bool
	identityIndexCond   string
	identityIndexFilter string
	hashedSubPlan       bool
	seqScanOnFacts      bool
	sorts               bool
}

// explainIdentityPage plans the production page query with the given keyset
// cursor (a nil cursor is the first page) and walks the plan tree.
func explainIdentityPage(
	ctx context.Context, t *testing.T, db *sql.DB, cursor any, cursorFactID string,
) identityPagePlan {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(ctx, `EXPLAIN (FORMAT JSON) `+listActiveContainerImageIdentityFactsQuery,
		cursor, cursorFactID, listFactsByKindPageSize).Scan(&raw); err != nil {
		t.Fatalf("explain page query: %v", err)
	}
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v (%d plans)\n%s", err, len(plans), raw)
	}
	plan := identityPagePlan{raw: string(raw)}
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		plan.nodeTypes = append(plan.nodeTypes, nodeType)
		relation, _ := node["Relation Name"].(string)
		index, _ := node["Index Name"].(string)
		filter, _ := node["Filter"].(string)
		cond, _ := node["Index Cond"].(string)
		if nodeType == "Index Scan" && index == "fact_records_identity_epoch_idx_v2" {
			plan.identityIndexScan = true
			plan.identityIndexCond = cond
			plan.identityIndexFilter = filter
		}
		if nodeType == "Seq Scan" && relation == "fact_records" {
			plan.seqScanOnFacts = true
		}
		if nodeType == "Sort" {
			plan.sorts = true
		}
		if strings.Contains(filter, "hashed SubPlan") {
			plan.hashedSubPlan = true
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				if m, ok := child.(map[string]any); ok {
					walk(m)
				}
			}
		}
	}
	walk(plans[0].Plan)
	return plan
}

// TestIdentityPageQueryPlanRidesOrderedIndexLive pins the plan shape of the
// identity page query on a real server (#7805), for the first page and for a
// mid-load page. Both must read fact_records through the ordered partial index
// fact_records_identity_epoch_idx_v2 under the LIMIT, with the
// active-generation restriction as a hashed SubPlan filter, and must not sort
// the active set or sequentially scan fact_records. The mid-load page must also
// carry the keyset comparison as an Index Cond: as a Filter every page would
// rescan from the start of the index and the load would be quadratic again,
// which a first-page-only check cannot see.
//
// The rewrite from a JOIN to that filter depends on planner behavior ("OR
// FALSE" blocks the sublink pull-up). It was verified on PostgreSQL 18; this
// test is what pins it against a server upgrade or a query edit.
//
// Run locally with ESHU_IDENTITY_EPOCH_PROOF_DSN and
// ESHU_IDENTITY_EPOCH_PROOF_DISPOSABLE=1, or ESHU_POSTGRES_TEST_DSN.
func TestIdentityPageQueryPlanRidesOrderedIndexLive(t *testing.T) {
	db, ctx := openIdentityEpochProofSchema(t)
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("exec %.80q: %v", query, err)
		}
	}
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
	      SELECT 'scope-'||n, 'src', 'oci_registry', 'key-'||n, 'col', 'p'||n, now(), now(), 'active', 'gen-'||n||'-a' FROM generate_series(1,150) n`)
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
	      SELECT 'gen-'||n||'-'||g, 'scope-'||n, 'sched', now(), now(), CASE g WHEN 'a' THEN 'active' ELSE 'superseded' END
	      FROM generate_series(1,150) n, (VALUES ('a'),('s')) gs(g)`)
	exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
	      SELECT 'f-'||n||'-'||g||'-'||i, 'scope-'||n, 'gen-'||n||'-'||g, 'oci_registry.image_tag_observation',
	             'k'||n||'-'||g||'-'||i, 'oci_registry', 'sk'||i, now() - (random() * interval '30 days'), now(), '{}'::jsonb
	      FROM generate_series(1,150) n, (VALUES ('a',500),('s',400)) gs(g,cnt), LATERAL generate_series(1,gs.cnt) i`)
	exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
	      SELECT 'b-'||n||'-'||i, 'scope-'||n, 'gen-'||n||'-a', 'file', 'b'||n||'-'||i, 'git', 'bk'||i, now(), now(), '{}'::jsonb
	      FROM generate_series(1,150) n, generate_series(1,800) i`)
	exec(`ANALYZE ingestion_scopes`)
	exec(`ANALYZE scope_generations`)
	exec(`ANALYZE fact_records`)

	// A mid-load cursor: the 30,000th active identity fact in keyset order.
	var cursorObservedAt time.Time
	var cursorFactID string
	if err := db.QueryRowContext(ctx,
		`SELECT observed_at, fact_id FROM fact_records
		  WHERE fact_kind = 'oci_registry.image_tag_observation' AND generation_id LIKE '%-a'
		  ORDER BY observed_at, fact_id OFFSET 30000 LIMIT 1`).Scan(&cursorObservedAt, &cursorFactID); err != nil {
		t.Fatalf("pick mid-load cursor: %v", err)
	}

	pages := []struct {
		name         string
		cursor       any
		cursorFactID string
		wantKeyset   bool
	}{
		{name: "first page", cursor: nil, cursorFactID: ""},
		{name: "mid-load page", cursor: cursorObservedAt.UTC(), cursorFactID: cursorFactID, wantKeyset: true},
	}
	for _, page := range pages {
		plan := explainIdentityPage(ctx, t, db, page.cursor, page.cursorFactID)
		if !plan.identityIndexScan {
			t.Errorf("%s: no Index Scan on fact_records_identity_epoch_idx_v2; node types %v\n%s", page.name, plan.nodeTypes, plan.raw)
		}
		if !plan.hashedSubPlan {
			t.Errorf("%s: active generations are not filtered with a hashed SubPlan; node types %v\n%s", page.name, plan.nodeTypes, plan.raw)
		}
		if plan.seqScanOnFacts {
			t.Errorf("%s: plan sequentially scans fact_records; node types %v\n%s", page.name, plan.nodeTypes, plan.raw)
		}
		if plan.sorts {
			t.Errorf("%s: plan sorts the active set before the LIMIT (the quadratic shape); node types %v\n%s", page.name, plan.nodeTypes, plan.raw)
		}
		if page.wantKeyset {
			if !strings.Contains(plan.identityIndexCond, "observed_at") {
				t.Errorf("%s: the keyset comparison is not an Index Cond on the identity index (Index Cond %q, Filter %q): every page would rescan from the start\n%s",
					page.name, plan.identityIndexCond, plan.identityIndexFilter, plan.raw)
			}
			if strings.Contains(plan.identityIndexFilter, "observed_at") {
				t.Errorf("%s: the keyset comparison is evaluated as a Filter (%q)\n%s", page.name, plan.identityIndexFilter, plan.raw)
			}
		}
	}
}
