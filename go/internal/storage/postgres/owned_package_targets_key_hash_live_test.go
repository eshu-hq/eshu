// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9432 PostgreSQL 18.6 proof that the owned-package reader projects the same key_hash_low24 the window statement hashes with, so the vulnerability planner groups slot scopes on the reader's hash without rehashing in Go; isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"crypto/sha256"
	"testing"
)

// ownedPackageSlotKeyHashLow24 recomputes the reader statement's key hash
// for one (ecosystem, package, version) identity: sha256 of the
// pipe-joined slot key, low 24 bits in get_byte order. The test uses it as
// an independent oracle for the KeyHashLow24 column the reader returns; the
// planner itself never hashes.
func ownedPackageSlotKeyHashLow24(ecosystem, packageName, version string) uint32 {
	sum := sha256.Sum256([]byte(ecosystem + "|" + packageName + "|" + version))
	return uint32(sum[0]) | uint32(sum[1])<<8 | uint32(sum[2])<<16
}

// TestListOwnedPackageDependencyTargetsReportsKeyHashLive proves the
// planner's grouping input matches the statement's hash input: every target
// the reader returns carries the low 24 bits of sha256(eco|pkg|ver) the
// numbered_targets CTE computes for its slot.
func TestListOwnedPackageDependencyTargetsReportsKeyHashLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_keyhash_9432", "ESHU_POSTGRES_DSN is not set")
	if _, err := db.ExecContext(ctx, slotLiveFixture(4, 3)); err != nil {
		t.Fatalf("seed owned-package slot fixture: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})

	page := readSlotLiveWindow(ctx, t, store, 0)
	if len(page.Targets) != 7 {
		t.Fatalf("window targets = %d, want 7 (P=1 holds the whole fixture)", len(page.Targets))
	}
	for _, target := range page.Targets {
		want := ownedPackageSlotKeyHashLow24(target.Ecosystem, target.PackageName, target.Version)
		if target.KeyHashLow24 != want {
			t.Errorf("KeyHashLow24 for %s|%s|%s = %d, want %d",
				target.Ecosystem, target.PackageName, target.Version, target.KeyHashLow24, want)
		}
	}
}
