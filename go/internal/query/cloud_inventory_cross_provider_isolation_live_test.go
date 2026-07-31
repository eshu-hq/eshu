// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// TestCloudInventoryAccountAliasCrossProviderIsolationLive is the #5238 P1-A
// regression: account_id/project_id/subscription_id all resolve against the
// SAME shared canonical payload key ("account_id"), so nothing in the value
// itself disambiguates provider. AWS account ids and GCP project NUMBERS are
// both plain decimal strings, and accountIDFallback
// (go/internal/storage/postgres/cloud_inventory_evidence_gcp_project_id.go)
// can populate account_id from a numeric CAI full_resource_name segment for
// some asset types -- so this is a real collision class, not a hypothetical
// one. The HTTP/MCP layer closes it by rejecting an alias filter supplied
// without provider (TestCloudInventoryHandlerAccountAliasWithoutProviderRejected);
// this test proves the other half live against real Postgres: two canonical
// rows sharing the identical account_id value, one aws and one gcp, must
// NEVER co-mingle once provider correctly disambiguates them.
func TestCloudInventoryAccountAliasCrossProviderIsolationLive(t *testing.T) {
	db, ctx, cancel := openCloudInventoryLiveDB(t)
	defer cancel()
	seedCloudInventoryCrossProviderCollisionLiveCorpus(t, ctx, db)
	cr := NewContentReader(db)

	const collidingAccountID = "999999999999"

	awsOnly, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes:         true,
		Provider:          "aws",
		AccountAliasKey:   "account_id",
		AccountAliasValue: collidingAccountID,
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("provider=aws account_id-filtered read: %v", err)
	}
	awsUIDs := cloudInventoryUIDs(t, awsOnly.Resources)
	if !cloudInventoryEqualStringSets(awsUIDs, []string{"aws:s3:colliding-bucket"}) {
		t.Fatalf("provider=aws account_id=%s rows = %v, want exactly the aws row -- the gcp row sharing the same account_id value must never co-mingle",
			collidingAccountID, awsUIDs)
	}

	gcpOnly, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes:         true,
		Provider:          "gcp",
		AccountAliasKey:   "project_id",
		AccountAliasValue: collidingAccountID,
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("provider=gcp project_id-filtered read: %v", err)
	}
	gcpUIDs := cloudInventoryUIDs(t, gcpOnly.Resources)
	if !cloudInventoryEqualStringSets(gcpUIDs, []string{"gcp:compute:colliding-vm"}) {
		t.Fatalf("provider=gcp project_id=%s rows = %v, want exactly the gcp row -- the aws row sharing the same account_id value must never co-mingle",
			collidingAccountID, gcpUIDs)
	}
}

// seedCloudInventoryCrossProviderCollisionLiveCorpus seeds one AWS scope and
// one GCP scope, each with exactly one canonical resource, whose payloads
// carry the IDENTICAL account_id value across providers -- the numeric
// collision class TestCloudInventoryAccountAliasCrossProviderIsolationLive
// proves is safe once provider is required.
func seedCloudInventoryCrossProviderCollisionLiveCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	schemaStatements := []string{
		`CREATE TEMP TABLE ingestion_scopes (
          scope_id TEXT PRIMARY KEY,
          scope_kind TEXT NOT NULL,
          source_system TEXT NOT NULL,
          source_key TEXT NOT NULL,
          parent_scope_id TEXT NULL,
          collector_kind TEXT NOT NULL,
          partition_key TEXT NOT NULL,
          observed_at TIMESTAMPTZ NOT NULL,
          ingested_at TIMESTAMPTZ NOT NULL,
          status TEXT NOT NULL,
          active_generation_id TEXT NULL,
          payload JSONB NOT NULL DEFAULT '{}'::jsonb
        )`,
		`CREATE TEMP TABLE scope_generations (
          generation_id TEXT PRIMARY KEY,
          scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id),
          status TEXT NOT NULL,
          observed_at TIMESTAMPTZ NOT NULL,
          ingested_at TIMESTAMPTZ NOT NULL,
          trigger_kind TEXT NOT NULL
        )`,
		`CREATE TEMP TABLE fact_records (
          fact_id TEXT PRIMARY KEY,
          scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id),
          generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id),
          fact_kind TEXT NOT NULL,
          stable_fact_key TEXT NOT NULL,
          schema_version TEXT NOT NULL DEFAULT '0.0.0',
          collector_kind TEXT NOT NULL DEFAULT 'unknown',
          fencing_token BIGINT NOT NULL DEFAULT 0,
          source_confidence TEXT NOT NULL DEFAULT 'unknown',
          source_system TEXT NOT NULL,
          source_fact_key TEXT NOT NULL,
          source_uri TEXT NULL,
          source_record_id TEXT NULL,
          observed_at TIMESTAMPTZ NOT NULL,
          ingested_at TIMESTAMPTZ NOT NULL,
          is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
          payload JSONB NOT NULL DEFAULT '{}'::jsonb
        )`,
	}
	for i, statement := range schemaStatements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed schema statement %d: %v\n%s", i, err, statement)
		}
	}

	type seed struct {
		scopeID, generationID, provider, uid, resourceType string
	}
	seeds := []seed{
		{"aws:cloud:999999999999:us-east-1:s3", "gen-collide-aws", "aws", "aws:s3:colliding-bucket", "aws_s3_bucket"},
		{"gcp:cloud:999999999999:us-central1", "gen-collide-gcp", "gcp", "gcp:compute:colliding-vm", "compute.googleapis.com/Instance"},
	}
	const collidingAccountID = "999999999999"

	for _, s := range seeds {
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'region', $2, $1, NULL, $2, $1, now(), now(), 'active', $3, '{}'::jsonb)
`, s.scopeID, s.provider, s.generationID); err != nil {
			t.Fatalf("seed ingestion_scopes %s: %v", s.scopeID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status, observed_at, ingested_at, trigger_kind)
VALUES ($1, $2, 'active', now(), now(), 'snapshot')
`, s.generationID, s.scopeID); err != nil {
			t.Fatalf("seed scope_generations %s: %v", s.generationID, err)
		}

		payload, err := json.Marshal(map[string]any{
			"reducer_domain":        "cloud_inventory_admission",
			"scope_id":              s.scopeID,
			"generation_id":         s.generationID,
			"cloud_resource_uid":    s.uid,
			"provider":              s.provider,
			"resource_type":         s.resourceType,
			"account_id":            collidingAccountID,
			"management_origin":     "observed",
			"has_declared_evidence": false,
			"has_applied_evidence":  false,
			"has_observed_evidence": true,
		})
		if err != nil {
			t.Fatalf("marshal fact payload for %s: %v", s.uid, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
    collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
    observed_at, ingested_at, is_tombstone, payload
) VALUES ($1, $2, $3, 'reducer_cloud_resource_identity', $1, 1, $4, 0, 'inferred',
    'reducer', $1, now(), now(), false, $5::jsonb)
`, "f-collide-"+s.provider, s.scopeID, s.generationID, s.provider, string(payload)); err != nil {
			t.Fatalf("seed fact_records for %s: %v", s.uid, err)
		}
	}
}
