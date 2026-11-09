// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"sort"
	"strings"
)

// oversizedMemberUnionThreshold is the member-union size above which a
// group counts on
// eshu_dp_advisory_group_projection_member_union_oversized_total. The
// group still projects in full: the counter is the operator signal that
// alias grouping produced an unusually wide group, never a truncation.
const oversizedMemberUnionThreshold = 32

// BuildGroup folds one advisory group's qualifying winner rows into the
// desired graph state the writer reconciles.
//
// Members are the union of every row's upstream members plus each row's
// own CVE and advisory ids, so a singleton group whose winner carries no
// upstream list still names both of its ids. The union splits into CVE
// and advisory classes with canonicalCVEKey. Edges are one per
// (repository, package): when rows for the same package disagree on
// (ecosystem, purl) — two winners at different versions, say — the
// lowest byte-order pair wins, because ecosystem and PURL are
// deterministic functions of the package and any pick must be stable
// across retries. Rows naming no repository or package mint no edge:
// the edge identity is (repository, package), and a winner row missing
// either cannot name one.
func BuildGroup(groupID string, rows []WinnerRow) Group {
	cves := map[string]struct{}{}
	advisories := map[string]struct{}{}
	addMember := func(raw string) {
		member := strings.TrimSpace(raw)
		if member == "" {
			return
		}
		if key, ok := canonicalCVEKey(member); ok {
			cves[key] = struct{}{}
			return
		}
		advisories[member] = struct{}{}
	}

	type edgeKey struct {
		repositoryID string
		packageID    string
	}
	edgesByKey := map[edgeKey]Edge{}
	for _, row := range rows {
		for _, member := range row.Members {
			addMember(member)
		}
		addMember(row.CVEID)
		addMember(row.AdvisoryID)

		repositoryID := strings.TrimSpace(row.RepositoryID)
		packageID := strings.TrimSpace(row.PackageID)
		if repositoryID == "" || packageID == "" {
			continue
		}
		candidate := Edge{
			RepositoryID: repositoryID,
			PackageID:    packageID,
			Ecosystem:    row.Ecosystem,
			PURL:         row.PURL,
		}
		key := edgeKey{repositoryID: repositoryID, packageID: packageID}
		if current, exists := edgesByKey[key]; !exists || edgeLess(candidate, current) {
			edgesByKey[key] = candidate
		}
	}

	edges := make([]Edge, 0, len(edgesByKey))
	for _, edge := range edgesByKey {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].RepositoryID != edges[j].RepositoryID {
			return edges[i].RepositoryID < edges[j].RepositoryID
		}
		return edges[i].PackageID < edges[j].PackageID
	})

	return Group{
		ID:          groupID,
		CVEIDs:      sortedMembers(cves),
		AdvisoryIDs: sortedMembers(advisories),
		Edges:       edges,
	}
}

// edgeLess orders two candidate edges for one (repository, package) by
// ecosystem, then PURL, in Go byte order.
func edgeLess(a, b Edge) bool {
	if a.Ecosystem != b.Ecosystem {
		return a.Ecosystem < b.Ecosystem
	}
	return a.PURL < b.PURL
}

func sortedMembers(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	members := make([]string, 0, len(set))
	for member := range set {
		members = append(members, member)
	}
	sort.Strings(members)
	return members
}

// canonicalCVEKey mirrors upstreamCVEKey in
// go/internal/reducer/supplychain/core/os_advisory_upstream_group.go
// exactly (that predicate is unexported, so it cannot be imported): the
// entry trimmed, with a case-insensitive CVE- prefix and something after
// it. DEBIAN-CVE-*, UBUNTU-CVE-*, and DSA-* are advisory class, not CVE
// class, or a DSA would also form a group under its DEBIAN-CVE twin. An
// entry whose prefix is exactly "CVE-" keeps its spelling; any other
// case fold upper-cases the whole id, matching the group keys the
// reducer formed when it grouped the records.
func canonicalCVEKey(entry string) (string, bool) {
	entry = strings.TrimSpace(entry)
	if len(entry) <= len("CVE-") || !strings.EqualFold(entry[:len("CVE-")], "CVE-") {
		return "", false
	}
	if entry[:len("CVE-")] == "CVE-" {
		return entry, true
	}
	return strings.ToUpper(entry), true
}
