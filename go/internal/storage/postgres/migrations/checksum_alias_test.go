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

func TestIsSupersededChecksumAcceptsBothEditedAliases(t *testing.T) {
	for _, recorded := range []string{migration093PR6785Checksum, migration093PR6923Checksum} {
		if !IsSupersededChecksum(migration093Path, recorded, migration093ShippedChecksum) {
			t.Fatalf("IsSupersededChecksum(093, %s, shipped) = false, want true", recorded)
		}
	}
}

func TestIsSupersededChecksumRejectsUnknownChecksum(t *testing.T) {
	if IsSupersededChecksum(migration093Path, migration093UnknownChecksum, migration093ShippedChecksum) {
		t.Fatal("IsSupersededChecksum(093, unknown, shipped) = true, want false")
	}
	if IsSupersededChecksum(migration093Path, migration093ShippedChecksum, migration093ShippedChecksum) {
		t.Fatal("the shipped checksum itself must never need alias treatment (it is not a mismatch)")
	}
}

func TestIsSupersededChecksumDoesNotLeakToOtherPaths(t *testing.T) {
	if IsSupersededChecksum(otherMigrationPathForAliasNeg, migration093PR6785Checksum, migration093ShippedChecksum) {
		t.Fatal("093's alias must not be accepted for a different migration path")
	}
	if IsSupersededChecksum(otherMigrationPathForAliasNeg, migration093PR6923Checksum, migration093ShippedChecksum) {
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
		if IsSupersededChecksum(migration093Path, recorded, migration093PR6785Checksum) {
			t.Fatalf("IsSupersededChecksum(093, %s, current=%s) = true, want false: current is not the shipped checksum",
				recorded, migration093PR6785Checksum)
		}
	}
}
