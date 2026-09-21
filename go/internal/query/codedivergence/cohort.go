// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"sort"
	"strings"
)

// OutlierCohort is one analyzed cohort: its definition source, its key
// (interface entity id, router mount, or package directory), a human label,
// and its member entity ids in deterministic order.
type OutlierCohort struct {
	// Source names the cohort definition that produced the cohort.
	Source CohortSource
	// Key is the cohort address: interface entity id, router mount, or package directory.
	Key string
	// Label is the human cohort name.
	Label string
	// Members are the analyzed member entity ids in deterministic order.
	Members []string
	// TotalMembers is the pre-truncation member count. It equals
	// len(Members) unless truncation cut the cohort; assembly reports the
	// difference, never silently samples.
	TotalMembers int
}

// OutlierCohortSeed is one cohort-membership row from a cohort enumeration
// read. Only the columns of the row's source are populated;
// GroupOutlierCohorts resolves the cohort key and label (endpoint-mount and
// package splits are string code shared with the content path, never Cypher
// dialect).
type OutlierCohortSeed struct {
	// Source names the enumeration read that produced the seed.
	Source CohortSource
	// IfaceID is the implemented interface's entity id (interface source).
	IfaceID string
	// IfaceName is the implemented interface's display name.
	IfaceName string
	// EndpointPath is the served endpoint path (router source).
	EndpointPath string
	// FilePath is the containing file's relative path (package source).
	FilePath string
	// MemberID is the cohort member's entity id.
	MemberID string
}

// routerMount groups endpoint paths by the mount one framework router
// serves: the first path segment (express Router.use('/widgets'), Rails
// scope). The root path mounts at "/".
func routerMount(endpointPath string) string {
	trimmed := strings.Trim(endpointPath, "/")
	if trimmed == "" {
		return "/"
	}
	if index := strings.Index(trimmed, "/"); index >= 0 {
		return trimmed[:index]
	}
	return trimmed
}

// GroupOutlierCohorts groups enumeration seeds into deterministic cohorts
// (source, key order; member ids sorted, truncated at the cap with the
// pre-truncation total kept for reporting). Cohorts below the minimum size
// drop with a counted below_min_cohort each; truncated members count under
// cohort_truncated. Nothing samples silently.
func GroupOutlierCohorts(seeds []OutlierCohortSeed, params OutlierParams) ([]OutlierCohort, map[string]int) {
	suppressions := map[string]int{}
	type cohortKey struct {
		source CohortSource
		key    string
	}
	labels := map[cohortKey]string{}
	members := map[cohortKey]map[string]struct{}{}
	for _, seed := range seeds {
		if seed.MemberID == "" {
			continue
		}
		var key, label string
		switch seed.Source {
		case CohortInterface:
			if seed.IfaceID == "" {
				continue
			}
			key, label = seed.IfaceID, seed.IfaceName
			if label == "" {
				label = seed.IfaceID
			}
		case CohortRouter:
			if seed.EndpointPath == "" {
				continue
			}
			mount := routerMount(seed.EndpointPath)
			key, label = mount, "/"+mount+" routes"
		default:
			if seed.FilePath == "" {
				continue
			}
			key, label = PackageOf(seed.FilePath), PackageOf(seed.FilePath)
		}
		ck := cohortKey{source: seed.Source, key: key}
		labels[ck] = label
		if members[ck] == nil {
			members[ck] = map[string]struct{}{}
		}
		members[ck][seed.MemberID] = struct{}{}
	}
	keys := make([]cohortKey, 0, len(members))
	for key := range members {
		keys = append(keys, key)
	}
	// Cohorts order by trust rank (interface, router, package), then key:
	// the deterministic order follows the issue's definition order, not
	// string collation.
	rank := map[CohortSource]int{CohortInterface: 0, CohortRouter: 1, CohortPackage: 2}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].source != keys[j].source {
			return rank[keys[i].source] < rank[keys[j].source]
		}
		return keys[i].key < keys[j].key
	})
	cohorts := make([]OutlierCohort, 0, len(keys))
	for _, key := range keys {
		ids := make([]string, 0, len(members[key]))
		for id := range members[key] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if len(ids) < params.MinCohortSize {
			suppressions[RuleBelowMinCohort]++
			continue
		}
		cohort := OutlierCohort{Source: key.source, Key: key.key, Label: labels[key], TotalMembers: len(ids)}
		if len(ids) > params.MaxCohortSize && params.MaxCohortSize > 0 {
			suppressions[RuleCohortTruncated] += len(ids) - params.MaxCohortSize
			ids = ids[:params.MaxCohortSize]
		}
		cohort.Members = ids
		cohorts = append(cohorts, cohort)
	}
	return cohorts, suppressions
}

// OutlierFingerprint addresses one outlier finding: the cohort source and
// key plus the majority callee id, NUL-joined like findingID inputs so
// member ids containing slashes cannot collide with the separator.
func OutlierFingerprint(source CohortSource, key, calleeID string) string {
	return string(source) + "\x00" + key + "\x00" + calleeID
}

// ParseOutlierFingerprint splits a convention_outlier fingerprint back into
// its cohort address and majority callee. It reports false on any other
// kind's fingerprint, so investigate never misreads a foreign address.
func ParseOutlierFingerprint(fingerprint string) (CohortSource, string, string, bool) {
	parts := strings.Split(fingerprint, "\x00")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	switch CohortSource(parts[0]) {
	case CohortInterface, CohortRouter, CohortPackage:
		return CohortSource(parts[0]), parts[1], parts[2], true
	default:
		return "", "", "", false
	}
}
