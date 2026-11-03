// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import "testing"

const (
	migration093Path              = "go/internal/storage/postgres/migrations/093_cross_scope_completion_queue.sql"
	migration093ShippedChecksum   = "c95cae2762bd4d0d42da4720eb0ad5545d2d032914bded15a65ab01acb92ce42"
	migration093PR6785Checksum    = "6cdb3e58545d3cbf13649402549ee7c1e577f99666bc2a5e0e87c1b9d0735e50"
	migration093PR6923Checksum    = "7f73153be9a782875b054085c6eafa9d7e6723507b343e2e5ec43b307168473c"
	migration093UnknownChecksum   = "0000000000000000000000000000000000000000000000000000000000000"
	otherMigrationPathForAliasNeg = "go/internal/storage/postgres/migrations/112_value_flow_refresh_producer_domains.sql"
)

const (
	migration004Path                    = "go/internal/storage/postgres/migrations/004_content_store.sql"
	migration004DeferredShippedChecksum = "3d8ef124d1e6b5641f5d84fe581ede18046fcde70f6aad5adb3934611562f5f3"
	migration004DeferredPre8028Checksum = "027b3563ab35a7824f5add728209998f530497a1ed3a7293844bd5e4ad53fdf4"
	migration004DeferredUnknownChecksum = "1111111111111111111111111111111111111111111111111111111111111111"
	migration004FullChecksum            = "236862da7ff45760fad77bd97d7bb82d323f0f3509e52032def81da77476b1cc"
)

func TestIsSupersededChecksumAcceptsBothEditedAliases(t *testing.T) {
	for _, recorded := range []string{migration093PR6785Checksum, migration093PR6923Checksum} {
		if !IsSupersededChecksum(migration093Path, "full", recorded, migration093ShippedChecksum) {
			t.Fatalf("IsSupersededChecksum(093, full, %s, shipped) = false, want true", recorded)
		}
	}
}

func TestIsSupersededChecksumRejectsUnknownChecksum(t *testing.T) {
	if IsSupersededChecksum(migration093Path, "full", migration093UnknownChecksum, migration093ShippedChecksum) {
		t.Fatal("IsSupersededChecksum(093, full, unknown, shipped) = true, want false")
	}
	if IsSupersededChecksum(migration093Path, "full", migration093ShippedChecksum, migration093ShippedChecksum) {
		t.Fatal("the shipped checksum itself must never need alias treatment (it is not a mismatch)")
	}
}

func TestIsSupersededChecksumDoesNotLeakToOtherPaths(t *testing.T) {
	if IsSupersededChecksum(otherMigrationPathForAliasNeg, "full", migration093PR6785Checksum, migration093ShippedChecksum) {
		t.Fatal("093's alias must not be accepted for a different migration path")
	}
	if IsSupersededChecksum(otherMigrationPathForAliasNeg, "full", migration093PR6923Checksum, migration093ShippedChecksum) {
		t.Fatal("093's alias must not be accepted for a different migration path")
	}
}

// TestIsSupersededChecksumRejectsAliasWhenCurrentDriftsFromShipped is the
// #7016 P2 regression: an alias must only ever be accepted when the file on
// disk is still exactly the shipped 093 bytes. If 093 were edited in place
// again (current no longer equals the shipped checksum), a ledger holding
// either old alias must NOT be silently accepted -- that would mask new,
// unrelated drift behind a stale exception.
func TestIsSupersededChecksumRejectsAliasWhenCurrentDriftsFromShipped(t *testing.T) {
	for _, recorded := range []string{migration093PR6785Checksum, migration093PR6923Checksum} {
		if IsSupersededChecksum(migration093Path, "full", recorded, migration093PR6785Checksum) {
			t.Fatalf("IsSupersededChecksum(093, full, %s, current=%s) = true, want false: current is not the shipped checksum",
				recorded, migration093PR6785Checksum)
		}
	}
}

// TestIsSupersededChecksumRejects093AliasUnderDeferredVariant pins the
// path+variant keying: the 093 entry lives under variant "full" only. A
// deferred-variant row must never match it, even when recorded and current
// are the exact aliased pair.
func TestIsSupersededChecksumRejects093AliasUnderDeferredVariant(t *testing.T) {
	for _, recorded := range []string{migration093PR6785Checksum, migration093PR6923Checksum} {
		if IsSupersededChecksum(migration093Path, "deferred", recorded, migration093ShippedChecksum) {
			t.Fatalf("IsSupersededChecksum(093, deferred, %s, shipped) = true, want false: 093's alias is full-variant only",
				recorded)
		}
	}
}

// TestIsSupersededChecksumAcceptsPre8028Deferred004 is the #9279 fix: a
// ledger whose 004 deferred row predates #8028 (recorded 027b3563...) must be
// accepted while the deferred SQL on disk is still the shipped 3d8ef124...
// constant.
func TestIsSupersededChecksumAcceptsPre8028Deferred004(t *testing.T) {
	if !IsSupersededChecksum(migration004Path, "deferred", migration004DeferredPre8028Checksum, migration004DeferredShippedChecksum) {
		t.Fatal("IsSupersededChecksum(004, deferred, pre-#8028, shipped) = false, want true")
	}
}

// TestIsSupersededChecksumRejectsDeferred004AliasUnderFullVariant is the
// variant-specific negative: the deferred alias must never rescue a full row,
// even when current equals the deferred shipped checksum.
func TestIsSupersededChecksumRejectsDeferred004AliasUnderFullVariant(t *testing.T) {
	if IsSupersededChecksum(migration004Path, "full", migration004DeferredPre8028Checksum, migration004DeferredShippedChecksum) {
		t.Fatal("IsSupersededChecksum(004, full, pre-#8028, deferred-shipped) = true, want false: the 004 alias is deferred-variant only")
	}
	if IsSupersededChecksum(migration004Path, "full", migration004DeferredPre8028Checksum, migration004FullChecksum) {
		t.Fatal("IsSupersededChecksum(004, full, pre-#8028, full-shipped) = true, want false")
	}
}

// TestIsSupersededChecksumRejectsDeferred004Drift pins the shipped guard for
// the 004 entry: if the deferred constant is edited in place again, the old
// alias must stop matching.
func TestIsSupersededChecksumRejectsDeferred004Drift(t *testing.T) {
	if IsSupersededChecksum(migration004Path, "deferred", migration004DeferredPre8028Checksum, migration004DeferredPre8028Checksum) {
		t.Fatal("IsSupersededChecksum(004, deferred, pre-#8028, current=pre-#8028) = true, want false: current is not the shipped checksum")
	}
	if IsSupersededChecksum(migration004Path, "deferred", migration004DeferredUnknownChecksum, migration004DeferredShippedChecksum) {
		t.Fatal("IsSupersededChecksum(004, deferred, unknown, shipped) = true, want false")
	}
	if IsSupersededChecksum(migration004Path, "deferred", migration004DeferredShippedChecksum, migration004DeferredShippedChecksum) {
		t.Fatal("the shipped checksum itself must never need alias treatment (it is not a mismatch)")
	}
}

// TestIsSupersededChecksumDoesNotLeak004AliasToOtherPaths mirrors the 093
// leak test for the new entry.
func TestIsSupersededChecksumDoesNotLeak004AliasToOtherPaths(t *testing.T) {
	if IsSupersededChecksum(otherMigrationPathForAliasNeg, "deferred", migration004DeferredPre8028Checksum, migration004DeferredShippedChecksum) {
		t.Fatal("004's alias must not be accepted for a different migration path")
	}
}
