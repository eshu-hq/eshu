// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9322 disposable PostgreSQL proof that Swift and
// OS-distribution derived OSV scopes reach readiness through recorded package
// keys: Swift keys already join once the instance enables swift, and
// distro-configured instances join canonical os keys
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"testing"
)

// TestSupplyChainImpactReadinessDerivedSwiftScopeMatchesLive is #9322 C3
// (Swift half): a swift-keyed derived OSV scope matches a Swift package
// request through the same package-key join as every other derived ecosystem.
func TestSupplyChainImpactReadinessDerivedSwiftScopeMatchesLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-swift", "swift")
	seedDerivedScope(t, ctx, db, now, "derived-swift",
		"vuln-intel://osv/swift/example-package?version=1.0.0",
		"swift", "fresh", "succeeded", [][2]string{{"swift", "example-package"}})

	readiness := readDerivedReadiness(t, db, "package_id=swift://swift-package-manager/example-package")
	state := onlyDerivedState(t, readiness)
	if state.ScopeCount != 1 || state.PackageCount != 1 || state.UnmatchedPackageCount != 0 ||
		state.FreshnessState != "fresh" || state.ScopeID != "derived://osv/swift" {
		t.Fatalf("derived aggregate = %+v, want one fresh matched swift scope", state)
	}
	// No top-level readiness assertion: a bare package_id anchor carries no
	// owned_packages evidence, so the envelope stays evidence_incomplete for
	// an orthogonal reason. The derived-row match above is the Swift proof.
}

// TestSupplyChainImpactReadinessDerivedDistroScopeMatchesLive is #9322 C3
// (OS half): a distro-configured instance's canonical os package keys match an
// OS package request, so OS-distribution derived scopes join the key table
// instead of reading silently unmatched.
func TestSupplyChainImpactReadinessDerivedDistroScopeMatchesLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-debian", "debian")
	seedDerivedScope(t, ctx, db, now, "derived-debian",
		"vuln-intel://osv/debian/zlib1g?version=1.0.0",
		"debian", "fresh", "succeeded", [][2]string{{"os", "zlib1g"}})

	readiness := readDerivedReadiness(t, db, "package_id=os://os/zlib1g")
	state := onlyDerivedState(t, readiness)
	if state.ScopeCount != 1 || state.PackageCount != 1 || state.UnmatchedPackageCount != 0 ||
		state.FreshnessState != "fresh" || state.ScopeID != "derived://osv/debian" {
		t.Fatalf("derived aggregate = %+v, want one fresh matched debian scope", state)
	}
	// No top-level readiness assertion: a bare package_id anchor carries no
	// owned_packages evidence, so the envelope stays evidence_incomplete for
	// an orthogonal reason. The derived-row match above is the OS proof.
}
