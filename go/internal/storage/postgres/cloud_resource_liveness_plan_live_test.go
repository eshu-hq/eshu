// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestCloudResourceLivenessProbePlanStaysOnIndexAtChunkSizeLive pins #6946:
// at a lock-chunk-sized candidate array the live probe must plan on the
// migration-118 partial index, not walk ingestion_scopes. It seeds 2,000
// active scopes x 100 admitted uids (plus a superseded generation each, so
// the join is not trivially selective), ANALYZEs, and asserts the EXPLAIN
// (ANALYZE, BUFFERS) plan of the production admission read
// (liveAdmissionCloudUIDsAliveSQL) for 500 candidates names the partial index and
// stays under a buffer budget far below the scope-walk plan (14,115 shared
// hits on this seed; the index plan is 1,346). Without migration 119's
// extended statistics the planner estimates ~2,000 rows per candidate and
// chooses the walk, which is the RED this test was written against.
//
// Skipped by default; set ESHU_CLOUD_RETRACT_LIVE=1 and ESHU_POSTGRES_DSN.
// Seeds under a unique prefix and deletes them at the end.
func TestCloudResourceLivenessProbePlanStaysOnIndexAtChunkSizeLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_LIVE")) == "" {
		t.Skip("set ESHU_CLOUD_RETRACT_LIVE=1 and ESHU_POSTGRES_DSN to run the cloud retract probe-plan proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	prefix := fmt.Sprintf("plan-%d", time.Now().UnixNano())
	const scopes, uidsPerScope, candidates = 2000, 100, 500
	seed := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		 SELECT $1||'-s-'||g, 'aws', 'aws', $1||'-s-'||g, 'aws', $1||'-s-'||g, now(), now(), 'active', NULL, '{}'::jsonb FROM generate_series(1, $2::int) g`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 SELECT $1||'-g-'||s||'-'||n, $1||'-s-'||s, 'manual', now(), now(), CASE WHEN n = 2 THEN 'active' ELSE 'superseded' END, now()
		 FROM generate_series(1, $2::int) s, generate_series(1, 2) n`,
		`UPDATE ingestion_scopes SET active_generation_id = $1||'-g-'||substr(scope_id, length($1) + 4)||'-2' WHERE scope_id LIKE $1||'-s-%'`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 SELECT $1||'-f-'||s||'-'||n||'-'||k, $1||'-s-'||s, $1||'-g-'||s||'-'||n, 'reducer_cloud_resource_identity', $1||'-k-'||s||'-'||k, 'aws', $1||'-k-'||s||'-'||k, now(), now(), FALSE,
		        jsonb_build_object('cloud_resource_uid', $1||'-uid-'||s||'-'||k)
		 FROM generate_series(1, $2::int) s, generate_series(1, 2) n, generate_series(1, $3::int) k`,
	}
	for i, stmt := range seed {
		args := []any{prefix, scopes}
		if i == 2 {
			args = []any{prefix}
		}
		if i == 3 {
			args = []any{prefix, scopes, uidsPerScope}
		}
		if _, err := database.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("seed step %d: %v", i, err)
		}
	}
	defer func() {
		_, _ = database.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id LIKE $1||'-s-%'`, prefix)
	}()
	// VACUUM (ANALYZE): the statistics migration 119 depends on are refreshed
	// and dead tuples from earlier seeds do not skew the planner's costs.
	if _, err := database.ExecContext(ctx, `VACUUM (ANALYZE) fact_records`); err != nil {
		t.Fatalf("vacuum analyze: %v", err)
	}
	uids := make([]string, 0, candidates)
	for i := 1; i <= candidates; i++ {
		if i%2 == 0 {
			uids = append(uids, fmt.Sprintf("%s-uid-%d-%d", prefix, i%scopes+1, i%uidsPerScope+1))
		} else {
			uids = append(uids, fmt.Sprintf("%s-dead-%d", prefix, i))
		}
	}
	rows, err := database.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+liveAdmissionCloudUIDsAliveSQL, uids)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	text := plan.String()
	if !strings.Contains(text, "fact_records_cloud_retract_admission_uid_idx") {
		t.Fatalf("500-candidate probe did not use the partial uid index:\n%s", text)
	}
	if strings.Contains(text, "Index Scan using fact_records_scope_generation_idx") {
		t.Fatalf("500-candidate probe walked ingestion_scopes into fact_records_scope_generation_idx (the #6946 plan flip):\n%s", text)
	}
	hits := topLevelSharedHits(text)
	const budget = 5000
	if hits <= 0 || hits > budget {
		t.Fatalf("500-candidate probe touched %d shared buffers, budget %d (scope walk is ~14,000):\n%s", hits, budget, text)
	}
	t.Logf("500-candidate probe: index plan, %d shared buffers", hits)
}

// topLevelSharedHits returns the first "Buffers: shared hit=N" figure in an
// EXPLAIN text plan, which is the root node's total.
func topLevelSharedHits(plan string) int {
	for _, line := range strings.Split(plan, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Buffers: shared hit=") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(strings.TrimPrefix(trimmed, "Buffers: shared hit="), "%d", &n); err == nil {
			return n
		}
	}
	return 0
}
