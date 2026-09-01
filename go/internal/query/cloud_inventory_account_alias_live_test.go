// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
//
// No build tag: this test must compile and run (skipping cleanly without a
// DSN) in the default `go test ./...` lane CI actually runs, matching the
// sibling live-proof precedent in this package (e.g.
// supply_chain_impact_runtime_filter_live_test.go). An earlier revision of
// this file carried `//go:build integration`, which no workflow, Makefile
// target, or script in this repo ever passes -- the test never ran anywhere.
func TestCloudInventoryAccountIDMatchesExactScopeIDLive(t *testing.T) {
	db, ctx, cancel := openCloudInventoryLiveDB(t)
	defer cancel()
	seedCloudInventoryAccountAliasLiveCorpus(t, ctx, db)
	cr := NewContentReader(db)

	const boundedLimit = 10

	// --- (1) single-scope account: account_id and exact scope_id agree exactly ---
	singleScopeAccount, err := cr.CloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes:         true,
		Provider:          "aws",
		AccountAliasKey:   "account_id",
		AccountAliasValue: "222222222222",
		Limit:             boundedLimit,
	})
	if err != nil {
		t.Fatalf("account_id (single-scope account) read: %v", err)
	}
	exactScope, err := cr.CloudInventoryIdentities(ctx, cloudInventoryFilter{
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
	multiScopeAccount, err := cr.CloudInventoryIdentities(ctx, cloudInventoryFilter{
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

	// One scope alone must NOT see the other scope's resource.
	oneOfTwoScopes, err := cr.CloudInventoryIdentities(ctx, cloudInventoryFilter{
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
	truncatedByAccount, err := cr.CloudInventoryIdentities(ctx, cloudInventoryFilter{
		AllScopes:         true,
		Provider:          "aws",
		AccountAliasKey:   "account_id",
		AccountAliasValue: "222222222222",
		Limit:             smallLimit,
	})
	if err != nil {
		t.Fatalf("account_id truncated-page read: %v", err)
	}
	truncatedByScope, err := cr.CloudInventoryIdentities(ctx, cloudInventoryFilter{
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

// TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive is the
// per-provider counterpart the initial fix skipped: GCP's project_id and
// Azure's subscription_id selectors must resolve non-vacuously too. Each
// provider gets its own single-scope account (mirroring the AWS single-claim
// shape above) so the alias and the exact scope_id selector must return the
// identical row set for each, live against Postgres.
func TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive(t *testing.T) {
	db, ctx, cancel := openCloudInventoryLiveDB(t)
	defer cancel()
	seedCloudInventoryAccountAliasLiveCorpus(t, ctx, db)
	cr := NewContentReader(db)

	tests := []struct {
		name      string
		provider  string
		aliasKey  string
		accountID string
		scopeID   string
		wantUIDs  []string
	}{
		{
			name: "gcp project_id", provider: "gcp", aliasKey: "project_id",
			accountID: "eshu-prod", scopeID: "gcp:cloud:eshu-prod:us-central1",
			wantUIDs: []string{"gcp:compute:vm-1", "gcp:compute:vm-2"},
		},
		{
			name: "azure subscription_id", provider: "azure", aliasKey: "subscription_id",
			accountID: "11111111-2222-3333-4444-555555555555", scopeID: "azure:cloud:11111111-2222-3333-4444-555555555555:eastus",
			wantUIDs: []string{"azure:vm:vm-1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			byAlias, err := cr.CloudInventoryIdentities(ctx, cloudInventoryFilter{
				AllScopes:         true,
				Provider:          tc.provider,
				AccountAliasKey:   tc.aliasKey,
				AccountAliasValue: tc.accountID,
				Limit:             10,
			})
			if err != nil {
				t.Fatalf("%s alias read: %v", tc.name, err)
			}
			byScope, err := cr.CloudInventoryIdentities(ctx, cloudInventoryFilter{
				AllScopes: true,
				Provider:  tc.provider,
				ScopeID:   tc.scopeID,
				Limit:     10,
			})
			if err != nil {
				t.Fatalf("%s exact scope_id read: %v", tc.name, err)
			}
			aliasUIDs := cloudInventoryUIDs(t, byAlias.Resources)
			scopeUIDs := cloudInventoryUIDs(t, byScope.Resources)
			if !cloudInventoryEqualStringSets(aliasUIDs, tc.wantUIDs) {
				t.Fatalf("%s: alias rows = %v, want %v", tc.name, aliasUIDs, tc.wantUIDs)
			}
			if !cloudInventoryEqualStringSets(aliasUIDs, scopeUIDs) {
				t.Fatalf("%s: alias rows %v != exact scope_id rows %v", tc.name, aliasUIDs, scopeUIDs)
			}
		})
	}
}

// openCloudInventoryLiveDB opens the live Postgres connection this file's
// tests share, skipping cleanly when ESHU_POSTGRES_TEST_DSN is unset.
func openCloudInventoryLiveDB(t *testing.T) (*sql.DB, context.Context, context.CancelFunc) {
	t.Helper()
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
	return db, ctx, cancel
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

// seedCloudInventoryAccountAliasLiveCorpus seeds ingestion scopes and
// canonical reducer_cloud_resource_identity facts shaped like real collector
// output for all three providers:
//   - AWS: two scopes for account 111111111111 (one per account+region+service
//     partition, matching go/internal/collector/awscloud/awsruntime/source.go's
//     scopeAndGeneration) plus one single-scope account 222222222222
//     mirroring the issue's single-claim reproduction.
//   - GCP: one single-scope project "eshu-prod".
//   - Azure: one single-scope subscription.
//
// Every fact row's payload carries "account_id" -- the field
// cloudInventoryAdmissionBasePayload (go/internal/reducer/
// cloud_inventory_admission_writer.go) now writes uniformly across providers,
// sourced from the resolving provider fact's own account_id/project_id/
// subscription_id identity field (go/internal/storage/postgres/
// cloud_inventory_evidence.go's cloudInventorySourceFactMapping.accountIDKey).
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
		scopeID, generationID, provider string
	}
	scopes := []scopeSeed{
		{"aws:cloud:111111111111:us-east-1:s3", "gen-1a", "aws"},
		{"aws:cloud:111111111111:us-west-2:s3", "gen-1b", "aws"},
		{"aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws"},
		{"gcp:cloud:eshu-prod:us-central1", "gen-3a", "gcp"},
		{"azure:cloud:11111111-2222-3333-4444-555555555555:eastus", "gen-4a", "azure"},
	}
	for _, s := range scopes {
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
	}

	type factSeed struct {
		factID, scopeID, generationID, uid, provider, resourceType, accountID string
	}
	facts := []factSeed{
		{"f-1a-1", "aws:cloud:111111111111:us-east-1:s3", "gen-1a", "aws:s3:bucket-1a", "aws", "aws_s3_bucket", "111111111111"},
		{"f-1a-2", "aws:cloud:111111111111:us-east-1:s3", "gen-1a", "aws:s3:bucket-1b", "aws", "aws_s3_bucket", "111111111111"},
		{"f-1b-1", "aws:cloud:111111111111:us-west-2:s3", "gen-1b", "aws:s3:bucket-1c", "aws", "aws_s3_bucket", "111111111111"},
		{"f-2a-1", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c1", "aws", "aws_s3_bucket", "222222222222"},
		{"f-2a-2", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c2", "aws", "aws_s3_bucket", "222222222222"},
		{"f-2a-3", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c3", "aws", "aws_s3_bucket", "222222222222"},
		{"f-2a-4", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c4", "aws", "aws_s3_bucket", "222222222222"},
		{"f-2a-5", "aws:cloud:222222222222:us-east-1:s3", "gen-2a", "aws:s3:bucket-c5", "aws", "aws_s3_bucket", "222222222222"},
		{"f-3a-1", "gcp:cloud:eshu-prod:us-central1", "gen-3a", "gcp:compute:vm-1", "gcp", "compute.googleapis.com/Instance", "eshu-prod"},
		{"f-3a-2", "gcp:cloud:eshu-prod:us-central1", "gen-3a", "gcp:compute:vm-2", "gcp", "compute.googleapis.com/Instance", "eshu-prod"},
		{
			"f-4a-1", "azure:cloud:11111111-2222-3333-4444-555555555555:eastus", "gen-4a", "azure:vm:vm-1",
			"azure", "Microsoft.Compute/virtualMachines", "11111111-2222-3333-4444-555555555555",
		},
	}
	for _, f := range facts {
		payload, err := json.Marshal(map[string]any{
			"reducer_domain":        "cloud_inventory_admission",
			"scope_id":              f.scopeID,
			"generation_id":         f.generationID,
			"cloud_resource_uid":    f.uid,
			"provider":              f.provider,
			"resource_type":         f.resourceType,
			"account_id":            f.accountID,
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
) VALUES ($1, $2, $3, 'reducer_cloud_resource_identity', $1, 1, $5, 0, 'inferred',
    'reducer', $1, now(), now(), false, $4::jsonb)
`, f.factID, f.scopeID, f.generationID, string(payload), f.provider); err != nil {
			t.Fatalf("seed fact_records %s: %v", f.factID, err)
		}
	}
	fmt.Fprintf(os.Stderr, "seeded %d scopes / %d canonical identity facts across aws/gcp/azure\n", len(scopes), len(facts))
}
