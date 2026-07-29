// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestCloudInventoryAccountIDMatchesExactScopeIDLive is the #5238 live-shaped
// regression: it seeds canonical reducer_cloud_resource_identity rows the same
// shape a real bounded AWS S3 collector run produces (see the issue's live
// proof table -- one claimed account+region+service partition, admitted
// canonical CloudResource identities, no synthetic scope_id equal to the raw
// account number), then proves:
//
//  1. account_id and its corresponding exact canonical scope_id return the
//     identical row set when the account has exactly one active scope (the
//     issue's reproduction shape: "One scheduled S3 claim was allowed").
//  2. account_id also correctly unions rows across MULTIPLE scopes sharing one
//     account (the general AWS shape: one scope per account+region+service
//     partition), which an exact scope_id selector -- by design -- does not.
//  3. A different account's rows never leak into the first account's page.
//  4. limit is honored identically by both selectors (holding limit constant
//     so a truncated 50-row default page is never mistaken for "empty").
func TestCloudInventoryAccountIDMatchesExactScopeIDLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live cloud inventory account-alias proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedCloudInventoryAccountAliasLiveCorpus(t, ctx, db)
	cr := NewContentReader(db)

	const boundedLimit = 10

	// --- (1) single-scope account: account_id and exact scope_id agree exactly ---
	singleScopeAccount, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes:         true,
		Provider:          "aws",
		AccountAliasKey:   "account_id",
		AccountAliasValue: "222222222222",
		Limit:             boundedLimit,
	})
	if err != nil {
		t.Fatalf("account_id (single-scope account) read: %v", err)
	}
	exactScope, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes: true,
		Provider:  "aws",
		ScopeID:   "aws:cloud:222222222222:us-east-1:s3",
		Limit:     boundedLimit,
	})
	if err != nil {
		t.Fatalf("exact scope_id read: %v", err)
	}
	singleScopeUIDs := cloudInventoryUIDs(t, singleScopeAccount.Resources)
	exactScopeUIDs := cloudInventoryUIDs(t, exactScope.Resources)
	if len(singleScopeUIDs) == 0 {
		t.Fatalf("account_id (single-scope account) returned 0 rows, want the 5 seeded canonical AWS resources")
	}
	if !cloudInventoryEqualStringSets(singleScopeUIDs, exactScopeUIDs) {
		t.Fatalf("account_id rows %v != exact scope_id rows %v; account_id must return the SAME row set as its corresponding canonical scope_id",
			singleScopeUIDs, exactScopeUIDs)
	}
	if got, want := len(singleScopeUIDs), 5; got != want {
		t.Fatalf("single-scope account row count = %d, want %d canonical AWS identities", got, want)
	}

	// --- (2) multi-scope account: account_id unions rows across every scope ---
	multiScopeAccount, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes:         true,
		Provider:          "aws",
		AccountAliasKey:   "account_id",
		AccountAliasValue: "111111111111",
		Limit:             boundedLimit,
	})
	if err != nil {
		t.Fatalf("account_id (multi-scope account) read: %v", err)
	}
	multiScopeUIDs := cloudInventoryUIDs(t, multiScopeAccount.Resources)
	wantMultiScope := []string{
		"aws:s3:bucket-1a", "aws:s3:bucket-1b", // scope A (us-east-1)
		"aws:s3:bucket-1c", // scope B (us-west-2)
	}
	if !cloudInventoryEqualStringSets(multiScopeUIDs, wantMultiScope) {
		t.Fatalf("multi-scope account_id rows = %v, want the union across both scopes %v", multiScopeUIDs, wantMultiScope)
	}

	// One scope alone must NOT see the other scope's resource (proves the fix
	// unions by account metadata, not by loosening the join to all scopes).
	oneOfTwoScopes, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes: true,
		Provider:  "aws",
		ScopeID:   "aws:cloud:111111111111:us-east-1:s3",
		Limit:     boundedLimit,
	})
	if err != nil {
		t.Fatalf("exact scope_id (one of two scopes) read: %v", err)
	}
	if got, want := len(cloudInventoryUIDs(t, oneOfTwoScopes.Resources)), 2; got != want {
		t.Fatalf("single-partition exact scope_id row count = %d, want %d (must not see the sibling scope's resource)", got, want)
	}

	// --- (3) cross-account isolation: a different account's rows never leak ---
	for _, uid := range multiScopeUIDs {
		if uid == "aws:s3:bucket-c1" || strings.Contains(uid, "222222222222") {
			t.Fatalf("account_id=111111111111 leaked a row belonging to account 222222222222: %v", multiScopeUIDs)
		}
	}
	for _, uid := range singleScopeUIDs {
		if strings.HasPrefix(uid, "aws:s3:bucket-1") {
			t.Fatalf("account_id=222222222222 leaked a row belonging to account 111111111111: %v", singleScopeUIDs)
		}
	}

	// --- (4) limit is honored identically by both selectors, holding it
	// constant, so truncation is never mistaken for account-alias correctness
	// or emptiness (the documented default-limit=50 trap). ---
	const smallLimit = 2
	truncatedByAccount, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes:         true,
		Provider:          "aws",
		AccountAliasKey:   "account_id",
		AccountAliasValue: "222222222222",
		Limit:             smallLimit,
	})
	if err != nil {
		t.Fatalf("account_id truncated-page read: %v", err)
	}
	truncatedByScope, err := cr.cloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes: true,
		Provider:  "aws",
		ScopeID:   "aws:cloud:222222222222:us-east-1:s3",
		Limit:     smallLimit,
	})
	if err != nil {
		t.Fatalf("exact scope_id truncated-page read: %v", err)
	}
	if got, want := len(truncatedByAccount.Resources), smallLimit; got != want {
		t.Fatalf("account_id page size = %d, want limit %d honored", got, want)
	}
	if got, want := len(truncatedByScope.Resources), smallLimit; got != want {
		t.Fatalf("exact scope_id page size = %d, want limit %d honored", got, want)
	}
	if truncatedByAccount.NextCursor == "" {
		t.Fatalf("account_id truncated page must report next_cursor when more rows remain")
	}
	if truncatedByScope.NextCursor == "" {
		t.Fatalf("exact scope_id truncated page must report next_cursor when more rows remain")
	}
	if !cloudInventoryEqualStringSets(
		cloudInventoryUIDs(t, truncatedByAccount.Resources),
		cloudInventoryUIDs(t, truncatedByScope.Resources),
	) {
		t.Fatalf("first page of account_id %v must equal first page of exact scope_id %v under the same deterministic ordering and limit",
			cloudInventoryUIDs(t, truncatedByAccount.Resources), cloudInventoryUIDs(t, truncatedByScope.Resources))
	}
}

func cloudInventoryUIDs(t *testing.T, resources []map[string]any) []string {
	t.Helper()
	uids := make([]string, 0, len(resources))
	for _, envelope := range resources {
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatalf("envelope missing payload object: %#v", envelope)
		}
		uid, ok := payload["cloud_resource_uid"].(string)
		if !ok || uid == "" {
			t.Fatalf("payload missing cloud_resource_uid: %#v", payload)
		}
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	return uids
}

func cloudInventoryEqualStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := append([]string(nil), a...)
	sortedB := append([]string(nil), b...)
	sort.Strings(sortedA)
	sort.Strings(sortedB)
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}

// seedCloudInventoryAccountAliasLiveCorpus seeds three ingestion scopes shaped
// like real AWS collector output:
//   - two scopes for account 111111111111 (one per account+region+service
//     partition, matching go/internal/collector/awscloud/awsruntime/source.go's
//     scopeAndGeneration, which derives scope_id per claim target and stores the
//     raw account_id only in the scope's own metadata payload)
//   - one scope for account 222222222222, mirroring the issue's single-claim
//     reproduction (one scheduled S3 claim, one scope, N canonical rows)
//
// Every fact row is a reducer_cloud_resource_identity canonical identity
// payload in the exact shape cloudInventoryAdmissionBasePayload
// (go/internal/reducer/cloud_inventory_admission_writer.go) writes.
func seedCloudInventoryAccountAliasLiveCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
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

	type scopeSeed struct {
		scopeID, generationID, accountID, region string
	}
	scopes := []scopeSeed{
		{"aws:cloud:111111111111:us-east-1:s3", "gen-1a", "111111111111", "us-east-1"},
		{"aws:cloud:111111111111:us-west-2:s3", "gen-1b", "111111111111", "us-west-2"},
		{"aws:cloud:222222222222:us-east-1:s3", "gen-2a", "222222222222", "us-east-1"},
	}
	for _, s := range scopes {
		metadata, err := json.Marshal(map[string]string{
			"account_id":   s.accountID,
			"region":       s.region,
			"service_kind": "s3",
		})
		if err != nil {
			t.Fatalf("marshal scope metadata: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'region', 'aws_cloud', $1, NULL, 'aws', $1, now(), now(), 'active', $2, $3::jsonb)
`, s.scopeID, s.generationID, string(metadata)); err != nil {
			t.Fatalf("seed ingestion_scopes %s: %v", s.scopeID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status, observed_at, ingested_at, trigger_kind)
VALUES ($1, $2, 'active', now(), now(), 'snapshot')
`, s.generationID, s.scopeID); err != nil {
			t.Fatalf("seed scope_generations %s: %v", s.generationID, err)
		}
	}

	type factSeed struct {
		factID, scopeID, generationID, uid string
	}
	facts := []factSeed{
		{"f-1a-1", "aws:cloud:111111111111:us-east-1:s3", "gen-1a", "aws:s3:bucket-1a"},
		{"f-1a-2", "aws:cloud:111111111111:us-east-1:s3", "gen-1a", "aws:s3:bucket-1b"},
		{"f-1b-1", "aws:cloud:111111111111:us-west-2:s3", "gen-1b", "aws:s3:bucket-1c"},
		{"f-2a-1", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c1"},
		{"f-2a-2", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c2"},
		{"f-2a-3", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c3"},
		{"f-2a-4", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c4"},
		{"f-2a-5", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c5"},
	}
	for _, f := range facts {
		payload, err := json.Marshal(map[string]any{
			"reducer_domain":        "cloud_inventory_admission",
			"scope_id":              f.scopeID,
			"generation_id":         f.generationID,
			"cloud_resource_uid":    f.uid,
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
`, f.factID, f.scopeID, f.generationID, string(payload)); err != nil {
			t.Fatalf("seed fact_records %s: %v", f.factID, err)
		}
	}
	fmt.Fprintf(os.Stderr, "seeded %d scopes / %d canonical AWS identity facts\n", len(scopes), len(facts))
}
