// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// TestCloudInventoryPreFixPayloadRolloutWindowLive is the #5238 rollout-window
// regression demanded by review: cloudInventoryAdmissionFactID
// (go/internal/reducer/cloud_inventory_admission_writer.go) hashes
// {scope_id, generation_id, cloud_resource_uid} -- AccountID is never part of
// that identity, so a scope's ALREADY-ADMITTED, currently-active generation
// (admitted by the reducer code that predates this fix) keeps its existing
// fact_id and is never re-written by this change. Its canonical payload has NO
// "account_id" key at all -- not present-and-blank, genuinely absent, the
// exact shape production data is in in the window between this fix deploying
// and that scope's next collector sync.
//
// This proves the documented rollout behavior (docs/public/reference/
// http-api.md's Cloud Inventory Readback section, go/internal/reducer/
// README.md's "Rollout window" note) against a real Postgres jsonb read:
//  1. account_id/project_id/subscription_id filters return ZERO rows against
//     a pre-fix-shaped row (payload->>'account_id' on a missing key is SQL
//     NULL, and NULL = $1 is never true) -- exactly the #5238 symptom,
//     reproduced for old data until a fresh sync re-admits it.
//  2. The exact canonical scope_id selector is UNAFFECTED and still returns
//     the row -- callers who already knew to use the workaround stay unbroken.
//  3. An unfiltered (provider-only) read still returns the row -- it was never
//     invisible, only unreachable through the newly-fixed alias selectors.
func TestCloudInventoryPreFixPayloadRolloutWindowLive(t *testing.T) {
	db, ctx, cancel := openCloudInventoryLiveDB(t)
	defer cancel()
	seedCloudInventoryPreFixPayloadLiveCorpus(t, ctx, db)
	cr := NewContentReader(db)

	const scopeID = "aws:cloud:333333333333:us-east-1:s3"

	byAccountID, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes:         true,
		Provider:          "aws",
		AccountAliasKey:   "account_id",
		AccountAliasValue: "333333333333",
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("account_id-filtered read: %v", err)
	}
	if got, want := len(byAccountID.Resources), 0; got != want {
		t.Fatalf("account_id-filtered read against a pre-fix-shaped row returned %d rows, want %d "+
			"(payload has no account_id key -- this is the documented rollout-window gap)", got, want)
	}

	byScopeID, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes: true,
		Provider:  "aws",
		ScopeID:   scopeID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("exact scope_id read: %v", err)
	}
	if got, want := len(cloudInventoryUIDs(t, byScopeID.Resources)), 1; got != want {
		t.Fatalf("exact scope_id read against a pre-fix-shaped row returned %d rows, want %d "+
			"-- scope_id must stay unaffected by the rollout window", got, want)
	}

	unfiltered, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes: true,
		Provider:  "aws",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("unfiltered provider=aws read: %v", err)
	}
	uids := cloudInventoryUIDs(t, unfiltered.Resources)
	if got, want := uids, []string{"aws:s3:pre-fix-bucket"}; !cloudInventoryEqualStringSets(got, want) {
		t.Fatalf("unfiltered provider=aws rows = %v, want %v -- a pre-fix row must stay visible without an account alias filter", got, want)
	}
}

// seedCloudInventoryPreFixPayloadLiveCorpus seeds one canonical
// reducer_cloud_resource_identity row whose payload has NO "account_id" key --
// the exact shape cloudInventoryAdmissionBasePayload
// (go/internal/reducer/cloud_inventory_admission_writer.go) produced before
// this fix added that field. cloudInventoryAdmissionFactID never included
// AccountID in its identity hash, so this is not a hypothetical: it is
// literally what every already-admitted, still-active generation looks like
// on deploy day until its scope's next collector sync re-admits it.
func seedCloudInventoryPreFixPayloadLiveCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
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

	const scopeID = "aws:cloud:333333333333:us-east-1:s3"
	const generationID = "gen-pre-fix-1"
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'region', 'aws_cloud', $1, NULL, 'aws', $1, now(), now(), 'active', $2, '{}'::jsonb)
`, scopeID, generationID); err != nil {
		t.Fatalf("seed ingestion_scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status, observed_at, ingested_at, trigger_kind)
VALUES ($1, $2, 'active', now(), now(), 'snapshot')
`, generationID, scopeID); err != nil {
		t.Fatalf("seed scope_generations: %v", err)
	}

	// Deliberately no "account_id" key anywhere in this payload -- this is the
	// exact cloudInventoryAdmissionBasePayload shape written before #5238's
	// fix. Every other field mirrors a real admitted row.
	payload, err := json.Marshal(map[string]any{
		"reducer_domain":        "cloud_inventory_admission",
		"scope_id":              scopeID,
		"generation_id":         generationID,
		"cloud_resource_uid":    "aws:s3:pre-fix-bucket",
		"provider":              "aws",
		"resource_type":         "aws_s3_bucket",
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
) VALUES ($1, $2, $3, 'reducer_cloud_resource_identity', $1, 1, 'aws', 0, 'inferred',
    'reducer', $1, now(), now(), false, $4::jsonb)
`, "f-pre-fix-1", scopeID, generationID, string(payload)); err != nil {
		t.Fatalf("seed fact_records: %v", err)
	}
}
