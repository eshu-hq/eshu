// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// seedDuplicateARNFacts inserts aws_resource facts for two ARNs where arn:dup
// carries two distinct payloads (duplicate ARN rows, as the evidence
// loader's first-wins dedup shows they reach this code) plus arn:single
// once. fact_ids interleave so physical order never matches ARN order.
func seedDuplicateARNFacts(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	rows := []struct{ factID, arn, payload string }{
		{"fdup-b-1", "arn:dup", `{"arn":"arn:dup","v":"second"}`},
		{"fsingle-1", "arn:single", `{"arn":"arn:single","v":"only"}`},
		{"fdup-a-1", "arn:dup", `{"arn":"arn:dup","v":"first"}`},
	}
	for _, r := range rows {
		if _, err := db.ExecContext(ctx, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload) VALUES ($1,$2,$3,'aws_resource','k','aws','k','uri',$4,$4,FALSE,$5::jsonb)`,
			r.factID, scopeID, generationID, now, r.payload); err != nil {
			t.Fatalf("seed duplicate ARN fact %s: %v", r.factID, err)
		}
	}
}

// TestAWSDriftFingerprintCollapsesDuplicateARNsLive pins the P2 collapse
// rule (#11018 review): same-ARN distinct-payload rows collapse to one
// per-ARN identity, identically in SQL and Go, so the maintenance listing
// and the recorded row converge instead of reopening every pass. The SQL
// fingerprint scalar over the generation must equal the Go twin over the
// loaded snapshot map, byte-for-byte, regardless of physical row order.
func TestAWSDriftFingerprintCollapsesDuplicateARNsLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftIsolatedLiveDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	suffix := fmt.Sprintf("dup-%d", time.Now().UnixNano())
	scopeID := "aws:" + suffix
	generationID := "gen-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", now)
	seedDuplicateARNFacts(t, ctx, sqlDB, scopeID, generationID)

	loader := PostgresAWSCloudRuntimeDriftEvidenceLoader{DB: SQLDB{DB: sqlDB}}
	snapshot, err := loader.LoadAWSDriftARNSnapshot(ctx, scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadAWSDriftARNSnapshot() error = %v, want nil", err)
	}
	if len(snapshot.ARNIDs) != 2 {
		t.Fatalf("snapshot ARNs = %v, want exactly [arn:dup arn:single] (one row per ARN)", snapshot.ARNIDs)
	}
	snapMap := map[string]string{}
	for i, arn := range snapshot.ARNIDs {
		snapMap[arn] = snapshot.ARNPayloadHashes[i]
	}
	want := reducercontract.ComputeARNInputFingerprint(snapMap)

	var got string
	if err := sqlDB.QueryRowContext(ctx,
		"SELECT ("+backfill.AWSDriftARNFingerprintSQL("$1", "$2")+")",
		scopeID, generationID).Scan(&got); err != nil {
		t.Fatalf("fingerprint scalar: %v", err)
	}
	if got != want {
		t.Fatalf("SQL fingerprint = %q, Go twin = %q: duplicate ARNs diverge", got, want)
	}

	// Determinism across physical orders: reseed the same logical rows
	// with swapped fact_ids in a second scope+generation (a second scope
	// keeps the one-active-generation-per-scope invariant); the
	// fingerprint must be identical.
	scopeID2 := "aws2:" + suffix
	generationID2 := "gen2-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID2, "aws", generationID2, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID2, scopeID2, "active", now)
	rows := []struct{ factID, arn, payload string }{
		{"fdup-a-2", "arn:dup", `{"arn":"arn:dup","v":"first"}`},
		{"fdup-b-2", "arn:dup", `{"arn":"arn:dup","v":"second"}`},
		{"fsingle-2", "arn:single", `{"arn":"arn:single","v":"only"}`},
	}
	for _, r := range rows {
		if _, err := sqlDB.ExecContext(ctx, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload) VALUES ($1,$2,$3,'aws_resource','k','aws','k','uri',$4,$4,FALSE,$5::jsonb)`,
			r.factID, scopeID2, generationID2, now, r.payload); err != nil {
			t.Fatalf("seed gen2 fact %s: %v", r.factID, err)
		}
	}
	var got2 string
	if err := sqlDB.QueryRowContext(ctx,
		"SELECT ("+backfill.AWSDriftARNFingerprintSQL("$1", "$2")+")",
		scopeID2, generationID2).Scan(&got2); err != nil {
		t.Fatalf("fingerprint scalar gen2: %v", err)
	}
	if got2 != got {
		t.Fatalf("fingerprint not stable across physical row order: %q vs %q", got, got2)
	}
}
