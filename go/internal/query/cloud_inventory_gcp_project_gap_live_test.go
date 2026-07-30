// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive
// is the #5238 documented-gap regression demanded by review: unlike AWS
// account_id and Azure subscription_id (both required identity fields on their
// source facts), GCP's project_id is genuinely OPTIONAL
// (sdk/go/factschema/gcp/v1/resource.go) -- an organization- or folder-level
// Cloud Asset Inventory asset has no project at all. The reducer's
// accountIDFallback (go/internal/storage/postgres/
// cloud_inventory_evidence_gcp_project_id.go) closes the DERIVABLE case (a
// blank project_id whose full_resource_name still embeds "projects/<id>"),
// proven by TestPostgresCloudInventoryEvidenceLoaderGCPBlankProjectIDDerivesFromFullResourceName.
// This test proves the remaining, genuinely unclosable case live: a canonical
// row admitted with account_id="" (an org-level asset with no derivable
// project segment) is correctly excluded from a project_id-filtered read, but
// still visible under an unscoped provider=gcp read -- documented behavior,
// not a silent hole.
func TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive(t *testing.T) {
	db, ctx, cancel := openCloudInventoryLiveDB(t)
	defer cancel()
	seedCloudInventoryGCPOrgLevelAssetLiveCorpus(t, ctx, db)
	cr := NewContentReader(db)

	byProjectID, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes:         true,
		Provider:          "gcp",
		AccountAliasKey:   "project_id",
		AccountAliasValue: "some-unrelated-project",
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("project_id-filtered read: %v", err)
	}
	if got, want := len(byProjectID.Resources), 0; got != want {
		t.Fatalf("project_id-filtered read returned %d rows, want %d -- an org-level asset with no project must never match an unrelated project_id", got, want)
	}

	unscoped, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes: true,
		Provider:  "gcp",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("unscoped provider=gcp read: %v", err)
	}
	uids := cloudInventoryUIDs(t, unscoped.Resources)
	if got, want := uids, []string{"gcp:org:policy-1"}; !cloudInventoryEqualStringSets(got, want) {
		t.Fatalf("unscoped provider=gcp rows = %v, want %v -- the org-level asset must still be visible without a project_id filter", got, want)
	}
}

// seedCloudInventoryGCPOrgLevelAssetLiveCorpus seeds one GCP organization-level
// scope and one canonical resource admitted from it with account_id="" -- the
// exact shape go/internal/reducer/cloud_inventory_admission_writer.go now
// persists for a gcp_cloud_resource fact whose full_resource_name has no
// "projects/<id>" segment (an org/folder-level Cloud Asset Inventory asset).
func seedCloudInventoryGCPOrgLevelAssetLiveCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
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

	const scopeID = "gcp:cloud:org:123456"
	const generationID = "gen-org-1"
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'account', 'gcp', $1, NULL, 'gcp', $1, now(), now(), 'active', $2, '{}'::jsonb)
`, scopeID, generationID); err != nil {
		t.Fatalf("seed ingestion_scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status, observed_at, ingested_at, trigger_kind)
VALUES ($1, $2, 'active', now(), now(), 'snapshot')
`, generationID, scopeID); err != nil {
		t.Fatalf("seed scope_generations: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"reducer_domain":     "cloud_inventory_admission",
		"scope_id":           scopeID,
		"generation_id":      generationID,
		"cloud_resource_uid": "gcp:org:policy-1",
		"provider":           "gcp",
		"resource_type":      "cloudresourcemanager.googleapis.com/Organization",
		// account_id="" is exactly what the reducer persists for a
		// gcp_cloud_resource fact whose full_resource_name carries no
		// "projects/<id>" segment (accountIDFallback returns "" too -- see
		// TestPostgresCloudInventoryEvidenceLoaderGCPBlankProjectIDWithNoDerivableSegment).
		"account_id":            "",
		"management_origin":     "observed",
		"has_declared_evidence": false,
		"has_applied_evidence":  false,
		"has_observed_evidence": true,
	})
	if err != nil {
		t.Fatalf("marshal fact payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
    collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
    observed_at, ingested_at, is_tombstone, payload
) VALUES ($1, $2, $3, 'reducer_cloud_resource_identity', $1, 1, 'gcp', 0, 'inferred',
    'reducer', $1, now(), now(), false, $4::jsonb)
`, "f-org-1", scopeID, generationID, string(payload)); err != nil {
		t.Fatalf("seed fact_records: %v", err)
	}
}
