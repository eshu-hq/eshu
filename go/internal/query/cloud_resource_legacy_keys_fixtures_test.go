// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"testing"
)

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
          is_tombstone boolean NOT NULL, payload jsonb NOT NULL DEFAULT '{}'::jsonb
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
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, is_tombstone, payload)
		 SELECT 'fact-' || lpad(value::text, 6, '0'),
		        CASE WHEN value % 2 = 1 THEN 'scope:allowed' ELSE 'scope:denied' END,
		        CASE WHEN value % 2 = 1 THEN 'generation:allowed' ELSE 'generation:denied' END,
		        false,
		        jsonb_build_object('resource_type', 'type-' || lpad((value % 20)::text, 2, '0'))
		 FROM generate_series(1, 20000) AS value`,
		`INSERT INTO graph_node_owner
         SELECT 'uid-' || lpad(value::text, 6, '0'),
                jsonb_build_object(
                  'source_fact_id', 'fact-' || lpad(value::text, 6, '0'),
                  'resource_type', 'type-' || lpad((value % 20)::text, 2, '0'),
                  'collector_kind', 'provider-' || lpad((value % 4)::text, 2, '0'),
                  'region', 'region-' || lpad((value % 8)::text, 2, '0'),
                  'account_id', 'account-' || lpad((value % 16)::text, 2, '0'),
                  'running_image_digest', 'sha256:' || lpad(to_hex(value), 64, '0'),
                  'arn', 'arn:example:compute:::resource/' || lpad(value::text, 6, '0')
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
		`CREATE INDEX graph_node_owner_cloud_resource_runtime_digest_idx
           ON graph_node_owner (((winning_row->>'running_image_digest')), ((winning_row->>'arn')), uid)
           WHERE winning_row->>'resource_type' IS NOT NULL
             AND NULLIF(BTRIM(winning_row->>'running_image_digest'), '') IS NOT NULL
             AND NULLIF(BTRIM(winning_row->>'arn'), '') IS NOT NULL`,
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
