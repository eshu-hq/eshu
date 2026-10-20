// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
)

// TestRetirementCaptureGraphKeysLive proves the 2c capture seam: the keys
// derive from the live rows (repository fact path wins, local_path then
// scope repo_path), store write-once, and read back the stored copy.
func TestRetirementCaptureGraphKeysLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_capture",
		"set ESHU_POSTGRES_DSN to run the retirement capture Postgres proof")
	sqlDB := SQLDB{DB: database}

	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		 observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ('cap-main', 'git', 'git', 'cap-key', 'git', 'PARTITION:cap',
		 NOW(), NOW(), 'active', 'cap-gen-1', '{"repo_path": "/fallback/cap"}'::jsonb),
		       ('cap-ref', 'git', 'git', 'cap-key', 'git', 'PARTITION:cap',
		 NOW(), NOW(), 'active', 'cap-gen-2', '{}'::jsonb)`); err != nil {
		t.Fatalf("seed scopes: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		VALUES ('cap-gen-1', 'cap-main', 'manual', NOW(), NOW(), 'active'),
		       ('cap-gen-2', 'cap-ref', 'manual', NOW(), NOW(), 'active')`); err != nil {
		t.Fatalf("seed generations: %v", err)
	}
	// The main scope's fact carries path; the @ref scope's fact carries
	// only local_path (the collector shape). Both resolve under one root.
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_records
		(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		 source_fact_key, observed_at, ingested_at, payload)
		VALUES ('cap-fact-1', 'cap-main', 'cap-gen-1', 'repository', 'cap-repo', 'git',
		 'cap-src-1', NOW(), NOW(), '{"path": "/repo/cap"}'::jsonb),
		       ('cap-fact-2', 'cap-ref', 'cap-gen-2', 'repository', 'cap-repo', 'git',
		 'cap-src-2', NOW(), NOW(), '{"local_path": "/repo/cap"}'::jsonb)`); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	store := NewRetirementStore(sqlDB)
	marker, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_capture_live", RepoID: "PARTITION:cap",
		ScopeID:    "cap-main",
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "capture-live-key",
	})
	if err != nil {
		t.Fatalf("InsertPending: %v", err)
	}

	keys, err := CaptureGraphRetractKeys(ctx, sqlDB, "PARTITION:cap")
	if err != nil {
		t.Fatalf("CaptureGraphRetractKeys: %v", err)
	}
	if len(keys.ScopeIDs) != 2 || keys.ScopeIDs[0] != "cap-main" || keys.ScopeIDs[1] != "cap-ref" {
		t.Fatalf("scope ids = %v, want [cap-main cap-ref]", keys.ScopeIDs)
	}
	if keys.Root != "/repo/cap" {
		t.Fatalf("root = %q, want /repo/cap (fact path wins)", keys.Root)
	}

	stored, err := StoreGraphRetractKeys(ctx, sqlDB, marker.RetirementID, keys)
	if err != nil || !stored {
		t.Fatalf("StoreGraphRetractKeys first = %v, %v; want true, nil", stored, err)
	}
	// Write-once: a second store with different values keeps the first.
	stored, err = StoreGraphRetractKeys(ctx, sqlDB, marker.RetirementID,
		GraphRetractKeys{ScopeIDs: []string{"other"}, Root: "/other"})
	if err != nil {
		t.Fatalf("StoreGraphRetractKeys second: %v", err)
	}
	if stored {
		t.Fatal("second store reported stored; the capture must be write-once")
	}
	readBack, err := ReadGraphRetractKeys(ctx, sqlDB, marker.RetirementID)
	if err != nil {
		t.Fatalf("ReadGraphRetractKeys: %v", err)
	}
	if readBack.Root != "/repo/cap" || len(readBack.ScopeIDs) != 2 {
		t.Fatalf("read back = %+v, want the first capture", readBack)
	}

	// Unknown repos fail closed instead of capturing an empty key set the
	// graph phase would then run unbounded.
	if _, err := CaptureGraphRetractKeys(ctx, sqlDB, "PARTITION:absent"); err == nil {
		t.Error("capture for an unknown repo must fail closed")
	}
	if _, err := CaptureGraphRetractKeys(ctx, sqlDB, "  "); err == nil {
		t.Error("capture for an empty repo must fail closed")
	}
}

// TestRetirementCaptureGraphKeysScopeFallbackLive proves the last
// precedence arm: with no repository fact at all, the scope payload's
// repo_path names the root.
func TestRetirementCaptureGraphKeysScopeFallbackLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_capture_fb",
		"set ESHU_POSTGRES_DSN to run the retirement capture fallback Postgres proof")
	sqlDB := SQLDB{DB: database}

	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		 observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ('capfb-main', 'git', 'git', 'capfb-key', 'git', 'PARTITION:capfb',
		 NOW(), NOW(), 'active', 'capfb-gen-1', '{"repo_path": "/fallback/capfb"}'::jsonb)`); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		VALUES ('capfb-gen-1', 'capfb-main', 'manual', NOW(), NOW(), 'active')`); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	keys, err := CaptureGraphRetractKeys(ctx, sqlDB, "PARTITION:capfb")
	if err != nil {
		t.Fatalf("CaptureGraphRetractKeys: %v", err)
	}
	if keys.Root != "/fallback/capfb" {
		t.Fatalf("root = %q, want the scope repo_path fallback", keys.Root)
	}
}
