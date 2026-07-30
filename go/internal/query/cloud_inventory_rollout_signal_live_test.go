// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// TestCloudInventoryPreRolloutEvidenceExistsLive proves
// cloudInventoryPreRolloutEvidenceExists' real Postgres jsonb semantics for
// the #5238 account-alias rollout-gap warning: the `?` key-exists operator
// distinguishes "no account_id key at all" (pre-fix payload shape) from
// "account_id key present" (post-fix payload shape), scoped correctly by
// provider and by access grant.
func TestCloudInventoryPreRolloutEvidenceExistsLive(t *testing.T) {
	db, ctx, cancel := openCloudInventoryLiveDB(t)
	defer cancel()
	seedCloudInventoryRolloutSignalLiveCorpus(t, ctx, db)
	cr := NewContentReader(db)

	// A pre-fix row exists in the aws scope, so an all-scopes aws probe must
	// report true.
	awsGap, err := cr.cloudInventoryPreRolloutEvidenceExists(ctx, cloudInventoryFilter{
		AllScopes: true,
		Provider:  "aws",
	})
	if err != nil {
		t.Fatalf("aws pre-rollout probe: %v", err)
	}
	if !awsGap {
		t.Fatal("aws pre-rollout probe = false, want true (a pre-fix aws row exists in scope)")
	}

	// Every gcp row in this corpus is post-fix (carries account_id), so a gcp
	// probe must report false -- a genuine "no gap, zero rows really means no
	// such account" case.
	gcpGap, err := cr.cloudInventoryPreRolloutEvidenceExists(ctx, cloudInventoryFilter{
		AllScopes: true,
		Provider:  "gcp",
	})
	if err != nil {
		t.Fatalf("gcp pre-rollout probe: %v", err)
	}
	if gcpGap {
		t.Fatal("gcp pre-rollout probe = true, want false (every gcp row in this corpus is post-fix)")
	}

	// A scoped caller granted only the post-fix aws scope must not see the
	// pre-fix row in the sibling aws scope: the probe must respect the same
	// access-scope predicate as the primary read.
	scopedAwsGap, err := cr.cloudInventoryPreRolloutEvidenceExists(ctx, cloudInventoryFilter{
		AllScopes:       false,
		Provider:        "aws",
		AllowedScopeIDs: []string{cloudInventoryRolloutSignalPostFixAWSScopeID},
	})
	if err != nil {
		t.Fatalf("scoped aws pre-rollout probe: %v", err)
	}
	if scopedAwsGap {
		t.Fatal("scoped aws pre-rollout probe = true, want false (grant excludes the pre-fix scope)")
	}
}

const (
	cloudInventoryRolloutSignalPreFixAWSScopeID  = "aws:cloud:111111111111:us-east-1:s3"
	cloudInventoryRolloutSignalPostFixAWSScopeID = "aws:cloud:222222222222:us-east-1:s3"
	cloudInventoryRolloutSignalPostFixGCPScopeID = "gcp:cloud:project-signal:global:compute"
)

// seedCloudInventoryRolloutSignalLiveCorpus seeds three scopes: one aws scope
// with a pre-fix payload (no account_id key, the shape
// TestCloudInventoryPreFixPayloadRolloutWindowLive already proves the primary
// read excludes), one aws scope with a post-fix payload (has account_id), and
// one gcp scope with a post-fix payload only -- so the gcp probe has no
// pre-fix row anywhere to find.
func seedCloudInventoryRolloutSignalLiveCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
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

	seedScope := func(scopeID, provider, generationID string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'region', $2, $1, NULL, $2, $1, now(), now(), 'active', $3, '{}'::jsonb)
`, scopeID, provider, generationID); err != nil {
			t.Fatalf("seed ingestion_scopes %s: %v", scopeID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status, observed_at, ingested_at, trigger_kind)
VALUES ($1, $2, 'active', now(), now(), 'snapshot')
`, generationID, scopeID); err != nil {
			t.Fatalf("seed scope_generations %s: %v", scopeID, err)
		}
	}
	seedFact := func(factID, scopeID, generationID, provider, uid string, accountID string) {
		t.Helper()
		payload := map[string]any{
			"reducer_domain":        "cloud_inventory_admission",
			"scope_id":              scopeID,
			"generation_id":         generationID,
			"cloud_resource_uid":    uid,
			"provider":              provider,
			"resource_type":         "resource_type",
			"management_origin":     "observed",
			"has_declared_evidence": false,
			"has_applied_evidence":  false,
			"has_observed_evidence": true,
		}
		// Deliberately omit the "account_id" key entirely when accountID is
		// blank -- the exact pre-fix payload shape, not present-and-blank.
		if accountID != "" {
			payload["account_id"] = accountID
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal fact payload %s: %v", factID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
    collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
    observed_at, ingested_at, is_tombstone, payload
) VALUES ($1, $2, $3, 'reducer_cloud_resource_identity', $1, 1, $4, 0, 'inferred',
    'reducer', $1, now(), now(), false, $5::jsonb)
`, factID, scopeID, generationID, provider, string(encoded)); err != nil {
			t.Fatalf("seed fact_records %s: %v", factID, err)
		}
	}

	seedScope(cloudInventoryRolloutSignalPreFixAWSScopeID, "aws", "gen-pre-fix-aws")
	seedFact("f-pre-fix-aws-1", cloudInventoryRolloutSignalPreFixAWSScopeID, "gen-pre-fix-aws", "aws", "aws:s3:pre-fix-bucket", "")

	seedScope(cloudInventoryRolloutSignalPostFixAWSScopeID, "aws", "gen-post-fix-aws")
	seedFact("f-post-fix-aws-1", cloudInventoryRolloutSignalPostFixAWSScopeID, "gen-post-fix-aws", "aws", "aws:s3:post-fix-bucket", "222222222222")

	seedScope(cloudInventoryRolloutSignalPostFixGCPScopeID, "gcp", "gen-post-fix-gcp")
	seedFact("f-post-fix-gcp-1", cloudInventoryRolloutSignalPostFixGCPScopeID, "gen-post-fix-gcp", "gcp", "gcp:compute:post-fix-instance", "project-signal")
}
