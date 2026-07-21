// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const cloudResourceListInteractiveSLO = 2 * time.Second

func TestCloudResourceListProductionVariantsLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live cloud resource page proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedCloudResourceListLiveCorpus(t, ctx, db)
	store := NewPostgresCloudResourceListStore(db)

	var maxDuration time.Duration
	for _, access := range cloudResourceListLiveAccessVariants() {
		for mask := 0; mask < 32; mask++ {
			filter := cloudResourceListLiveFilter(access.filter, mask)
			query, args := buildCloudResourceIdentityListQuery(filter)
			plan := explainCloudResourceListLiveQuery(t, ctx, db, query, args)
			if !strings.Contains(plan, "graph_node_owner_cloud_resource") {
				t.Errorf("%s/%02d owner page plan is not index-backed: %s", access.name, mask, plan)
			}

			start := time.Now()
			rows, err := store.ListCloudResourceIdentities(ctx, filter)
			duration := time.Since(start)
			if err != nil {
				t.Fatalf("%s/%02d list identities: %v", access.name, mask, err)
			}
			if duration > maxDuration {
				maxDuration = duration
			}
			if duration > cloudResourceListInteractiveSLO {
				t.Errorf("%s/%02d duration = %s, want <= %s", access.name, mask, duration, cloudResourceListInteractiveSLO)
			}
			assertCloudResourceListLiveRows(t, access, mask, filter, rows)
		}
	}
	assertCloudResourceListLivePaging(t, ctx, store)
	t.Logf("20,000-row production-variant max duration = %s (SLO %s)", maxDuration, cloudResourceListInteractiveSLO)
}

func assertCloudResourceListLivePaging(
	t *testing.T,
	ctx context.Context,
	store *PostgresCloudResourceListStore,
) {
	t.Helper()
	const pageSize = 137
	seen := make(map[string]struct{}, 20000)
	var previous CloudResourceListIdentity
	filter := CloudResourceListPageFilter{AllScopes: true, Limit: pageSize + 1}
	for {
		rows, err := store.ListCloudResourceIdentities(ctx, filter)
		if err != nil {
			t.Fatalf("page after %#v: %v", previous, err)
		}
		truncated := len(rows) > pageSize
		if truncated {
			rows = rows[:pageSize]
		}
		for _, row := range rows {
			if _, duplicate := seen[row.UID]; duplicate {
				t.Fatalf("duplicate page identity %q", row.UID)
			}
			if previous.UID != "" && (previous.ResourceType > row.ResourceType ||
				(previous.ResourceType == row.ResourceType && previous.UID >= row.UID)) {
				t.Fatalf("page boundary is not strictly ordered: %#v then %#v", previous, row)
			}
			seen[row.UID] = struct{}{}
			previous = row
		}
		if !truncated {
			break
		}
		filter.AfterResourceType = previous.ResourceType
		filter.AfterID = previous.UID
	}
	if got, want := len(seen), 20000; got != want {
		t.Fatalf("paged identities = %d, want %d with no gaps", got, want)
	}
}

type cloudResourceListLiveAccess struct {
	name     string
	filter   CloudResourceListPageFilter
	allowsID func(int) bool
}

func cloudResourceListLiveAccessVariants() []cloudResourceListLiveAccess {
	return []cloudResourceListLiveAccess{
		{
			name:     "all",
			filter:   CloudResourceListPageFilter{AllScopes: true},
			allowsID: func(int) bool { return true },
		},
		{name: "scoped", filter: CloudResourceListPageFilter{
			AllowedRepositoryIDs: []string{"repository:allowed"},
			AllowedScopeIDs:      []string{"scope:allowed"},
		}, allowsID: func(value int) bool { return value%2 == 1 }},
		{name: "scoped-no-match", filter: CloudResourceListPageFilter{
			AllowedRepositoryIDs: []string{"repository:missing"},
			AllowedScopeIDs:      []string{"scope:missing"},
		}, allowsID: func(int) bool { return false }},
	}
}

func cloudResourceListLiveFilter(base CloudResourceListPageFilter, mask int) CloudResourceListPageFilter {
	filter := base
	filter.Limit = 51
	if mask&1 != 0 {
		filter.Provider = "provider-01"
	}
	if mask&2 != 0 {
		filter.ResourceType = "type-10"
	}
	if mask&4 != 0 {
		filter.Region = "region-01"
	}
	if mask&8 != 0 {
		filter.AccountID = "account-01"
	}
	if mask&16 != 0 {
		filter.AfterResourceType = "type-05"
		filter.AfterID = "uid-010000"
	}
	return filter
}

func assertCloudResourceListLiveRows(
	t *testing.T,
	access cloudResourceListLiveAccess,
	mask int,
	filter CloudResourceListPageFilter,
	rows []CloudResourceListIdentity,
) {
	t.Helper()
	want := cloudResourceListLiveExpectedRows(access, filter)
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("%s/%02d rows differ\n got: %#v\nwant: %#v", access.name, mask, rows, want)
	}
	seen := make(map[string]struct{}, len(rows))
	for i, row := range rows {
		if _, duplicate := seen[row.UID]; duplicate {
			t.Errorf("%s/%02d duplicate uid %q", access.name, mask, row.UID)
		}
		seen[row.UID] = struct{}{}
		if i == 0 {
			continue
		}
		previous := rows[i-1]
		if previous.ResourceType > row.ResourceType ||
			(previous.ResourceType == row.ResourceType && previous.UID >= row.UID) {
			t.Errorf("%s/%02d rows are not strictly ordered at %d: %#v then %#v", access.name, mask, i, previous, row)
		}
	}
}

func cloudResourceListLiveExpectedRows(
	access cloudResourceListLiveAccess,
	filter CloudResourceListPageFilter,
) []CloudResourceListIdentity {
	rows := make([]CloudResourceListIdentity, 0, 20000)
	for value := 1; value <= 20000; value++ {
		if !access.allowsID(value) {
			continue
		}
		uid := fmt.Sprintf("uid-%06d", value)
		resourceType := fmt.Sprintf("type-%02d", value%20)
		if filter.Provider != "" && filter.Provider != fmt.Sprintf("provider-%02d", value%4) {
			continue
		}
		if filter.ResourceType != "" && filter.ResourceType != resourceType {
			continue
		}
		if filter.Region != "" && filter.Region != fmt.Sprintf("region-%02d", value%8) {
			continue
		}
		if filter.AccountID != "" && filter.AccountID != fmt.Sprintf("account-%02d", value%16) {
			continue
		}
		if filter.AfterID != "" && (resourceType < filter.AfterResourceType ||
			(resourceType == filter.AfterResourceType && uid <= filter.AfterID)) {
			continue
		}
		rows = append(rows, CloudResourceListIdentity{UID: uid, ResourceType: resourceType})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ResourceType != rows[j].ResourceType {
			return rows[i].ResourceType < rows[j].ResourceType
		}
		return rows[i].UID < rows[j].UID
	})
	if len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
	}
	return rows
}

func explainCloudResourceListLiveQuery(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	args []any,
) string {
	t.Helper()
	var plan string
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&plan); err != nil {
		t.Fatalf("EXPLAIN cloud resource page: %v", err)
	}
	return plan
}

func seedCloudResourceListLiveCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TEMP TABLE ingestion_scopes (
          scope_id text PRIMARY KEY, scope_kind text NOT NULL, source_key text NOT NULL,
          active_generation_id text
        )`,
		`CREATE TEMP TABLE scope_generations (
          generation_id text PRIMARY KEY, scope_id text NOT NULL, status text NOT NULL
        )`,
		`CREATE TEMP TABLE fact_records (
          fact_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
          is_tombstone boolean NOT NULL
        )`,
		`CREATE TEMP TABLE graph_node_owner (
          uid text PRIMARY KEY, winning_row jsonb NOT NULL
        )`,
		`INSERT INTO ingestion_scopes VALUES
		  ('scope:allowed', 'repository', 'repository:allowed', 'generation:allowed'),
		  ('scope:denied', 'repository', 'repository:denied', 'generation:denied')`,
		`INSERT INTO scope_generations VALUES
		  ('generation:allowed', 'scope:allowed', 'active'),
		  ('generation:denied', 'scope:denied', 'active')`,
		`INSERT INTO fact_records
		 SELECT 'fact-' || lpad(value::text, 6, '0'),
		        CASE WHEN value % 2 = 1 THEN 'scope:allowed' ELSE 'scope:denied' END,
		        CASE WHEN value % 2 = 1 THEN 'generation:allowed' ELSE 'generation:denied' END,
		        false
		 FROM generate_series(1, 20000) AS value`,
		`INSERT INTO graph_node_owner
         SELECT 'uid-' || lpad(value::text, 6, '0'),
                jsonb_build_object(
                  'source_fact_id', 'fact-' || lpad(value::text, 6, '0'),
                  'resource_type', 'type-' || lpad((value % 20)::text, 2, '0'),
                  'collector_kind', 'provider-' || lpad((value % 4)::text, 2, '0'),
                  'region', 'region-' || lpad((value % 8)::text, 2, '0'),
                  'account_id', 'account-' || lpad((value % 16)::text, 2, '0')
                )
         FROM generate_series(1, 20000) AS value`,
		`CREATE INDEX graph_node_owner_cloud_resource_page_idx
           ON graph_node_owner (((winning_row->>'resource_type')), uid)
           WHERE winning_row->>'resource_type' IS NOT NULL`,
		`CREATE INDEX graph_node_owner_cloud_resource_provider_page_idx
           ON graph_node_owner (((winning_row->>'collector_kind')), ((winning_row->>'resource_type')), uid)
           WHERE winning_row->>'resource_type' IS NOT NULL`,
		`CREATE INDEX graph_node_owner_cloud_resource_region_page_idx
           ON graph_node_owner (((winning_row->>'region')), ((winning_row->>'resource_type')), uid)
           WHERE winning_row->>'resource_type' IS NOT NULL`,
		`CREATE INDEX graph_node_owner_cloud_resource_account_page_idx
           ON graph_node_owner (((winning_row->>'account_id')), ((winning_row->>'resource_type')), uid)
           WHERE winning_row->>'resource_type' IS NOT NULL`,
		`ANALYZE ingestion_scopes`,
		`ANALYZE scope_generations`,
		`ANALYZE fact_records`,
		`ANALYZE graph_node_owner`,
	}
	for i, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed statement %d: %v\n%s", i, err, statement)
		}
	}
}
