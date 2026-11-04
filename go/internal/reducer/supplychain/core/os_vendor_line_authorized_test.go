// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestAuthorizedBranchFilterDropsForeignLineVendorFacts pins the second reader
// of vendor facts (#9945). The authorized-scan conflict pass picks the fix that
// applies to the host from the advisory's branches, so a vendor branch for
// another release must not contend there: its fix is that release's, not the
// host's. A branch with no readable line contends exactly as it did before, and
// an empty host line disables the filter.
func TestAuthorizedBranchFilterDropsForeignLineVendorFacts(t *testing.T) {
	t.Parallel()
	const dir = "pkg:deb/debian/curl?distro="
	same := vendorLineFact("debian", dir+"debian-12", "1:1.0-3")
	other := vendorLineFact("debian", dir+"debian-11", "1:1.0-3")
	unknown := vendorLineFact("debian", "pkg:deb/debian/curl", "1:1.0-3")
	osv := osvDebianCurlFact() // tagged for debian:12, 13 and 14

	got := filterAuthorizedBranchesToHostLine([]supplychainmodel.AffectedPackage{same, other, unknown, osv}, "debian:12")
	want := []supplychainmodel.AffectedPackage{same, unknown, osv}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("host debian:12 kept %+v, want %+v", got, want)
	}
	all := []supplychainmodel.AffectedPackage{same, other, unknown}
	if got := filterAuthorizedBranchesToHostLine(all, ""); !reflect.DeepEqual(got, all) {
		t.Fatalf("an empty host line must keep every branch, got %+v", got)
	}
}
