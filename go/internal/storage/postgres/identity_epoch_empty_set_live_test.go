// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8607 disposable PostgreSQL 18 proof, on the migrated schema in a private schema, that the epoch probe returns the empty-set epoch instead of a scan failure when the image-identity set is globally empty
//eshu:readiness:env ESHU_IDENTITY_EPOCH_PROOF_DISPOSABLE ESHU_IDENTITY_EPOCH_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"testing"
	"time"
)

// emptyIdentitySetFingerprint is the SHA-256 hex digest of the empty string:
// the probe digests the empty DISTINCT (scope_id, generation_id) pair set
// when no identity fact exists, so the empty-set epoch carries this
// fingerprint.
const emptyIdentitySetFingerprint = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// TestProbeIdentityEpochEmptySetLive is the #8607 regression: probing the
// epoch against a globally-empty image-identity set must return the empty-set
// epoch (count 0, zero max observed_at, empty-string digest) instead of
// failing in rows.Scan. The probe's COALESCE(max(observed_at),
// '-infinity') fallback produced a timestamptz the pgx stdlib driver hands
// back as the string "-infinity", which does not convert into the time.Time
// the loader scans into, so every probe of an identity-empty database
// failed. The isolated schema starts with no fact rows at all; the seeded
// scope holds an active generation but no identity facts, so the filtered
// identity set is empty while ingestion_scopes is not.
//
// Run locally with ESHU_IDENTITY_EPOCH_PROOF_DSN and
// ESHU_IDENTITY_EPOCH_PROOF_DISPOSABLE=1, or ESHU_POSTGRES_TEST_DSN.
func TestProbeIdentityEpochEmptySetLive(t *testing.T) {
	sqlDB, ctx := openIdentityEpochProofSchema(t)
	stamp := time.Now().Format("20060102-150405.000")
	scopeID := "live-epoch-empty-set-" + stamp
	generationID := "live-epoch-empty-gen-" + stamp

	// An active scope/generation with no identity facts: the image-identity
	// set stays globally empty.
	seedIngestionScopeLive(t, sqlDB, scopeID, generationID)
	store := NewFactStore(SQLDB{DB: sqlDB})

	epoch, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch on empty identity set: %v", err)
	}
	if epoch.count != 0 {
		t.Fatalf("empty-set epoch count = %d, want 0", epoch.count)
	}
	if !epoch.maxObservedAt.IsZero() {
		t.Fatalf("empty-set epoch maxObservedAt = %v, want zero time (no identity rows exist)", epoch.maxObservedAt)
	}
	if epoch.activeFingerprint != emptyIdentitySetFingerprint {
		t.Fatalf("empty-set epoch fingerprint = %q, want sha256 of empty pair set %q", epoch.activeFingerprint, emptyIdentitySetFingerprint)
	}
	t.Logf("empty-set epoch: count=%d max_obs=%v fingerprint=%s", epoch.count, epoch.maxObservedAt, epoch.activeFingerprint)
}
