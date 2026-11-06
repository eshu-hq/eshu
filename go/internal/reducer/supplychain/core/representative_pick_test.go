// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// representativePickBothOrders runs representativeAffectedPackage in both fact
// orders and fails when the orders disagree. Every case below names the
// regression: the pick used to rank under the first fact's ecosystem, so a
// mixed-ecosystem group named a different fact per order (#10034 item 1).
func representativePickBothOrders(t *testing.T, group []supplychainmodel.AffectedPackage) supplychainmodel.AffectedPackage {
	t.Helper()
	forward := representativeAffectedPackage(group)
	reversed := slices.Clone(group)
	slices.Reverse(reversed)
	backward := representativeAffectedPackage(reversed)
	if backward.FactID != forward.FactID {
		t.Fatalf("representative depends on fact order: forward %q, reversed %q (group %+v)",
			forward.FactID, backward.FactID, group)
	}
	return forward
}

// TestRepresentativePickRanksUnderTheIdentityVendor pins ruling 9990-orderdep
// 1.3: the ranking table for a group comes from the vendor class the group's
// package identity names, not from string order and not from the first fact's
// ecosystem. The first two cases re-derive the dropped commit's shapes: the
// smallest-ecosystem rule picked OSV over the Ubuntu vendor fact and a blank
// NVD fact over the vendor's own fact, against the vendor-first principle in
// provenance_selection.go. Every case runs in both fact orders.
func TestRepresentativePickRanksUnderTheIdentityVendor(t *testing.T) {
	t.Parallel()
	fact := func(factID, source, advisoryID, ecosystem, packageID, purl string) supplychainmodel.AffectedPackage {
		return supplychainmodel.AffectedPackage{
			FactID: factID, Source: source, AdvisoryID: advisoryID,
			Ecosystem: ecosystem, PackageID: packageID, PURL: purl, Name: "curl",
		}
	}
	ubuntuID := "pkg:deb/ubuntu/curl"
	ubuntuPURL := ubuntuID + "?distro=ubuntu-22.04"
	for _, tc := range []struct {
		name  string
		group []supplychainmodel.AffectedPackage
		want  string
	}{
		{
			// Rejected rule wanted "other-osv": "deb" < "os" ranks ubuntu 999.
			name: "ubuntu vendor fact beats OSV across deb and os",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", "USN-1", "deb", ubuntuID, ubuntuPURL),
				fact("other-osv", "osv", "OSV-1", "os", ubuntuID, ubuntuPURL),
			},
			want: "own",
		},
		{
			// Rejected rule wanted "other-nvd": blank sorts first and reads
			// the language table. The identity names ubuntu, so the vendor
			// table applies once the fact carries the group's purl.
			name: "ubuntu vendor fact beats blank NVD fact with the group purl",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", "USN-1", "ubuntu", ubuntuID, ubuntuPURL),
				fact("other-nvd", "nvd", "CVE-1", "", ubuntuID, ubuntuPURL),
			},
			want: "own",
		},
		{
			name: "debian vendor fact beats OSV across deb and os",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "debian", "DSA-1", "deb", "pkg:deb/debian/curl", "pkg:deb/debian/curl?distro=debian-12"),
				fact("other-osv", "osv", "OSV-1", "os", "pkg:deb/debian/curl", "pkg:deb/debian/curl?distro=debian-12"),
			},
			want: "own",
		},
		{
			// alma has no ecosystem table of its own: ranking through an
			// ecosystem name would put OSV ahead of the vendor fact, so the
			// rank must read the vendor directly.
			name: "alma vendor fact beats OSV across rpm and os",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "alma", "ALSA-1", "rpm", "pkg:rpm/alma/curl", "pkg:rpm/alma/curl"),
				fact("other-osv", "osv", "OSV-1", "os", "pkg:rpm/alma/curl", "pkg:rpm/alma/curl"),
			},
			want: "own",
		},
		{
			name: "rocky vendor fact beats OSV across rpm and os",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "rocky", "RLSA-1", "rpm", "pkg:rpm/rocky/curl", "pkg:rpm/rocky/curl"),
				fact("other-osv", "osv", "OSV-1", "os", "pkg:rpm/rocky/curl", "pkg:rpm/rocky/curl"),
			},
			want: "own",
		},
		{
			// The rpm arm of osPackageVendorFromPURL knows only the alma and
			// rocky namespaces, so a redhat identity names no vendor through
			// it. The ranking still reads the rpm namespace itself: the
			// vendor's own fact must outrank every generic source there too.
			name: "redhat namespace identity ranks the redhat fact first",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "redhat", "RHSA-1", "rpm", "pkg:rpm/redhat/curl", "pkg:rpm/redhat/curl"),
				fact("other-osv", "osv", "OSV-1", "os", "pkg:rpm/redhat/curl", "pkg:rpm/redhat/curl"),
			},
			want: "own",
		},
		{
			name: "os package id identity names the vendor",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", "USN-1", "deb", "os://ubuntu/curl", ""),
				fact("other-osv", "osv", "OSV-1", "os", "os://ubuntu/curl", ""),
			},
			want: "own",
		},
		{
			// The package id names no vendor, but the facts' purls do: the
			// group still ranks under the vendor they agree on.
			name: "purls name the vendor when the package id is bare",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", "USN-1", "deb", "curl", ubuntuPURL),
				fact("other-osv", "osv", "OSV-1", "os", "curl", ubuntuPURL),
			},
			want: "own",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := representativePickBothOrders(t, tc.group); got.FactID != tc.want {
				t.Fatalf("representative = %q, want %q (group %+v)", got.FactID, tc.want, tc.group)
			}
		})
	}
}

// TestRepresentativePickKeepsTheAgreedTable pins criterion 2 of #10034: a
// group that agrees on the ecosystem ranks under it even when the package
// identity names a vendor, so the finding (whose id carries the
// representative's purl) stays byte-identical. The vendor rule governs groups
// that disagree; here it must not move the pick.
func TestRepresentativePickKeepsTheAgreedTable(t *testing.T) {
	t.Parallel()
	ubuntuID := "pkg:deb/ubuntu/curl"
	ubuntuPURL := ubuntuID + "?distro=ubuntu-22.04"
	fact := func(factID, source, advisoryID, ecosystem, purl string) supplychainmodel.AffectedPackage {
		return supplychainmodel.AffectedPackage{
			FactID: factID, Source: source, AdvisoryID: advisoryID,
			Ecosystem: ecosystem, PackageID: ubuntuID, PURL: purl, Name: "curl",
		}
	}
	for _, tc := range []struct {
		name  string
		group []supplychainmodel.AffectedPackage
		want  string
	}{
		{
			// The deb table ranks debian first and the vendor's ubuntu fact
			// 999th: the base pick, kept in both orders.
			name: "agreed deb keeps the debian table over the vendor fact",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", "USN-1", "deb", ubuntuPURL),
				fact("other", "debian", "DSA-1", "deb", ubuntuPURL),
			},
			want: "other",
		},
		{
			// Under "os" the ghsa and ubuntu sources tie at rank 1 and the
			// source string breaks the tie: the base pick, kept in both orders.
			name: "agreed os keeps the source-string tiebreak",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", "USN-1", "os", ubuntuPURL),
				fact("other", "ghsa", "GHSA-1", "os", ubuntuPURL),
			},
			want: "other",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := representativePickBothOrders(t, tc.group); got.FactID != tc.want {
				t.Fatalf("representative = %q, want %q (group %+v)", got.FactID, tc.want, tc.group)
			}
		})
	}
}

// TestRepresentativePickFallsBackWithoutAVendor pins the ruling's fallback
// chain when the package identity names no vendor: the one ecosystem the group
// agrees on, else "os" when every fact has OS shape, else the language table.
// No fact below carries a package id or purl, so no vendor is named anywhere.
func TestRepresentativePickFallsBackWithoutAVendor(t *testing.T) {
	t.Parallel()
	fact := func(factID, source, ecosystem string) supplychainmodel.AffectedPackage {
		return supplychainmodel.AffectedPackage{FactID: factID, Source: source, Ecosystem: ecosystem}
	}
	for _, tc := range []struct {
		name  string
		group []supplychainmodel.AffectedPackage
		want  string
	}{
		{
			name: "an agreed ecosystem keeps its table",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", "deb"),
				fact("other", "debian", "deb"),
			},
			want: "other",
		},
		{
			name: "a blank-only group keeps the language table",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", ""),
				fact("other", "nvd", ""),
			},
			want: "other",
		},
		{
			name: "disagreeing OS-shaped facts rank under os",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", "deb"),
				fact("other", "osv", "os"),
			},
			want: "own",
		},
		{
			// Neither spelling is OS-shaped and no identity names a vendor,
			// so the language table decides, exactly as a blank first fact
			// always has. Production facts carry a package id at minimum, so
			// this arm documents the degenerate input rather than a live path.
			name: "disagreeing non-OS facts rank under the language table",
			group: []supplychainmodel.AffectedPackage{
				fact("own", "ubuntu", "ubuntu"),
				fact("other", "nvd", ""),
			},
			want: "other",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := representativePickBothOrders(t, tc.group); got.FactID != tc.want {
				t.Fatalf("representative = %q, want %q (group %+v)", got.FactID, tc.want, tc.group)
			}
		})
	}
}

// TestRepresentativePickIgnoresFactOrderAcrossTheGridAxes sweeps the parity
// grid's ecosystem and source axes at the representative level, with a vendor
// identity on every group, and requires both fact orders to agree. It pins the
// issue's Expected without pinning any single pick: which fact wins on a
// given shape is the vendor-first table's answer, measured by the grid.
func TestRepresentativePickIgnoresFactOrderAcrossTheGridAxes(t *testing.T) {
	t.Parallel()
	hosts := []struct{ vendor, packageID, query string }{
		{"ubuntu", "pkg:deb/ubuntu/curl", "?distro=ubuntu-22.04"},
		{"debian", "pkg:deb/debian/curl", "?distro=debian-12"},
		{"alpine", "pkg:apk/alpine/curl", "?distro=alpine-3.18"},
		{"alma", "pkg:rpm/alma/curl", ""},
		{"rocky", "pkg:rpm/rocky/curl", ""},
	}
	ecos := []string{"deb", "dpkg", "", "os", "ubuntu", "debian", "apk", "alpine", "rpm", "alma", "rocky"}
	sources := []string{"nvd", "debian", "ubuntu", "alpine", "glad", "ghsa", "", "osv", "redhat", "alma"}
	for _, h := range hosts {
		for _, ownEco := range ecos {
			for _, otherEco := range ecos {
				for _, src := range sources {
					own := supplychainmodel.AffectedPackage{
						FactID: "own", Source: h.vendor, AdvisoryID: "ADV-own",
						Ecosystem: ownEco, PackageID: h.packageID, PURL: h.packageID + h.query, Name: "curl",
					}
					other := supplychainmodel.AffectedPackage{
						FactID: "other", Source: src, AdvisoryID: "ADV-other",
						Ecosystem: otherEco, PackageID: h.packageID, PURL: h.packageID + h.query, Name: "curl",
					}
					group := []supplychainmodel.AffectedPackage{own, other}
					if got := representativeAffectedPackage(group); got.FactID != representativeAffectedPackage([]supplychainmodel.AffectedPackage{other, own}).FactID {
						t.Fatalf("host %s own=%q other=%q src=%q: orders disagree", h.vendor, ownEco, otherEco, src)
					}
				}
			}
		}
	}
}

// TestRepresentativePickIgnoresFactOrderWithoutIdentities sweeps ecosystem and
// source pairs with no package id or purl anywhere and requires both fact
// orders to agree, pinning order-independence on the fallback arms too.
func TestRepresentativePickIgnoresFactOrderWithoutIdentities(t *testing.T) {
	t.Parallel()
	ecos := []string{"", "apk", "deb", "debian", "dpkg", "npm", "os", "rpm", "ubuntu"}
	sources := []string{"", "alpine", "debian", "ghsa", "glad", "nvd", "osv", "ubuntu"}
	for _, ecoA := range ecos {
		for _, ecoB := range ecos {
			for _, srcA := range sources {
				for _, srcB := range sources {
					a := supplychainmodel.AffectedPackage{FactID: "a", Source: srcA, Ecosystem: ecoA}
					b := supplychainmodel.AffectedPackage{FactID: "b", Source: srcB, Ecosystem: ecoB}
					if got := representativeAffectedPackage([]supplychainmodel.AffectedPackage{a, b}); got.FactID != representativeAffectedPackage([]supplychainmodel.AffectedPackage{b, a}).FactID {
						t.Fatalf("eco %q/%q src %q/%q: orders disagree", ecoA, ecoB, srcA, srcB)
					}
				}
			}
		}
	}
}
