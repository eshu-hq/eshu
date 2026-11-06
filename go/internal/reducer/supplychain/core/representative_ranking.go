// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// representativeRanking is the order-independent ranking table for one
// affected_package group. When every fact agrees on the ecosystem the group
// ranks under it, exactly as before, so an agreed group keeps its finding
// byte-identical in every order (criterion 2 of #10034: the finding id carries
// the representative's purl, and a new pick would churn finding identities
// that exist today). vendor holds when a disagreeing group's package identity
// names an OS vendor class; otherwise the group ranks under ecosystem, which
// is "os" for an all-OS-shape group or "" for the language table.
type representativeRanking struct {
	vendor    string
	ecosystem string
}

// representativeSourceRank ranks one classified advisory source under the
// group's ranking table. A named vendor ranks through vendorClassPriorityRank
// directly, not through an ecosystem name: alma and rocky have no ecosystem
// table of their own, and advisorySourcePriority of either name would read the
// language table and put NVD ahead of the vendor's own fact.
func representativeSourceRank(ranking representativeRanking, classifiedSource string) int {
	if ranking.vendor != "" {
		return vendorClassPriorityRank(ranking.vendor, classifiedSource)
	}
	return advisorySourcePriority(ranking.ecosystem, classifiedSource)
}

// representativeRankingForGroup derives the ranking table for one group from
// the evidence the group carries, the same group in any fact order. A group
// that agrees on the ecosystem keeps its table: the representative's purl
// names the finding, so a new pick there would move finding identities that
// exist today, against criterion 2 of #10034.
//
// A group that disagrees ranks under the vendor class its package identity
// names (ruling 9990-orderdep 1.3). The group is keyed by package id, so the
// package id is the identity: it names the vendor first. An os:// package id
// names its vendor segment; a purl names its vendor through
// osPackageVendorFromPURL, which canonicalizes the alma and rocky distro
// namespaces. A purl whose OS-family namespace that helper does not know (the
// rpm arm knows only alma and rocky: a pkg:rpm/redhat/* identity names no
// vendor through it) names its own namespace, because the vendor rank only
// tests source equality and the vendor's own fact still outranks every
// generic source there. osPackageVendorFromPURL itself is unchanged: widening
// it would move the join keys built from it, which is #10034 item 3's scope,
// not this lane's.
//
// When the package id names no vendor, the facts' purls vote and the smallest
// vendor wins so the answer stays order-independent; a group whose facts name
// different vendors is the different-vendor limit tracked separately in
// #10034. With no vendor anywhere, the group ranks under "os" when every fact
// has OS shape, or under the language table otherwise.
func representativeRankingForGroup(packages []supplychainmodel.AffectedPackage) representativeRanking {
	first := packageidentity.LowerASCII(strings.TrimSpace(packages[0].Ecosystem))
	agreed := true
	for _, pkg := range packages[1:] {
		if packageidentity.LowerASCII(strings.TrimSpace(pkg.Ecosystem)) != first {
			agreed = false
			break
		}
	}
	if agreed {
		return representativeRanking{ecosystem: first}
	}
	if vendor := representativeVendorFromIdentity(packages[0].PackageID); vendor != "" {
		return representativeRanking{vendor: vendor}
	}
	vendor := ""
	for _, pkg := range packages {
		candidate := representativeVendorFromIdentity(pkg.PURL)
		if candidate == "" {
			continue
		}
		if vendor == "" || candidate < vendor {
			vendor = candidate
		}
	}
	if vendor != "" {
		return representativeRanking{vendor: vendor}
	}
	for _, pkg := range packages {
		if !affectedFactHasOSShape(pkg) {
			return representativeRanking{}
		}
	}
	return representativeRanking{ecosystem: string(packageidentity.EcosystemOS)}
}

// representativeVendorFromIdentity reads the OS vendor class a package
// identity names: a purl through osPackageVendorFromPURL, an os:// package id
// through its vendor segment, or, for a purl in an OS family whose namespace
// osPackageVendorFromPURL does not map, the namespace itself. It returns ""
// when the identity names no vendor.
func representativeVendorFromIdentity(identity string) string {
	if vendor := osPackageVendorFromPURL(identity); vendor != "" {
		return vendor
	}
	if purlType, namespace := osPackagePURLTypeAndNamespace(identity); namespace != "" {
		switch purlType {
		case "deb", "apk", "rpm":
			return namespace
		}
	}
	if rest, ok := strings.CutPrefix(packageidentity.LowerASCII(strings.TrimSpace(identity)), "os://"); ok {
		if vendor, _, _ := strings.Cut(rest, "/"); vendor != "" {
			return vendor
		}
	}
	return ""
}
