// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
)

func TestReconcileSupplyChainScannerIdentityDigestAgreement(t *testing.T) {
	digest := "sha256:aaaabbbbccccddddeeeeffff000011112222333344445555666677778888"
	identities := map[string]supplyChainImageIdentity{
		digest: {
			factID:              "identity-1",
			digest:              digest,
			sourceRepositoryIDs: []string{"repository:r_test"},
			canonicalWrites:     1,
		},
	}
	missing := reconcileSupplyChainScannerIdentityDigest(digest, "repository:r_test", identities)
	if len(missing) != 0 {
		t.Errorf("agreement should produce no missing evidence, got %v", missing)
	}
}

func TestReconcileSupplyChainScannerIdentityDigestMismatch(t *testing.T) {
	scannerDigest := "sha256:aaaabbbbccccddddeeeeffff000011112222333344445555666677778888"
	ciDigest := "sha256:9999888877776666555544443333222211110000aaaabbbbccccddddeeeeffff"
	repoID := "repository:r_test"

	identities := map[string]supplyChainImageIdentity{
		scannerDigest: {
			factID:              "identity-scanner",
			digest:              scannerDigest,
			sourceRepositoryIDs: []string{repoID},
			canonicalWrites:     1,
		},
		ciDigest: {
			factID:              "identity-ci",
			digest:              ciDigest,
			sourceRepositoryIDs: []string{repoID},
			canonicalWrites:     1,
		},
	}
	missing := reconcileSupplyChainScannerIdentityDigest(scannerDigest, repoID, identities)
	if len(missing) == 0 {
		t.Fatal("mismatch should produce missing evidence")
	}
	found := false
	for _, m := range missing {
		if m == "scanner_identity_digest_mismatch: scanner="+scannerDigest+", identity="+ciDigest {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing evidence should include mismatch entry, got %v", missing)
	}
}

func TestReconcileSupplyChainScannerIdentityDigestOneSideMissing(t *testing.T) {
	scannerDigest := "sha256:aaaabbbbccccddddeeeeffff000011112222333344445555666677778888"
	identities := map[string]supplyChainImageIdentity{
		"sha256:other": {
			factID:              "identity-other",
			digest:              "sha256:other",
			sourceRepositoryIDs: []string{"repository:r_other"},
			canonicalWrites:     1,
		},
	}
	// repoID set but no identity carries it — the loop iterates
	// and finds nothing.
	missing := reconcileSupplyChainScannerIdentityDigest(scannerDigest, "repository:r_missing", identities)
	if len(missing) != 0 {
		t.Errorf("repo with no matching identity should produce no missing evidence, got %v", missing)
	}
}

func TestReconcileSupplyChainScannerIdentityDigestAmbiguousRepo(t *testing.T) {
	// Two identities for the same repo — neither ambiguous (each has exactly
	// one sourceRepositoryIDs), but the scanner's digest matches a third
	// identity whose sourceRepositoryIDs has TWO entries (ambiguous). The
	// loop skips the ambiguous identity via len(identity.sourceRepositoryIDs)
	// != 1, so no mismatch is reported.
	scannerDigest := "sha256:aaaabbbbccccddddeeeeffff000011112222333344445555666677778888"
	ciDigest := "sha256:other"
	repoID := "repository:r_a"
	identities := map[string]supplyChainImageIdentity{
		scannerDigest: {
			factID:              "identity-scanner",
			digest:              scannerDigest,
			sourceRepositoryIDs: []string{repoID},
			canonicalWrites:     1,
		},
		ciDigest: {
			factID:              "identity-ambiguous",
			digest:              ciDigest,
			sourceRepositoryIDs: []string{"repository:r_a", "repository:r_b"},
			canonicalWrites:     1,
		},
	}
	missing := reconcileSupplyChainScannerIdentityDigest(scannerDigest, repoID, identities)
	if len(missing) != 0 {
		t.Errorf("ambiguous other identity should produce no missing evidence (len != 1 guard), got %v", missing)
	}
}
