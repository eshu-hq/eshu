// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

var knownDomains = knownDomainSet()

func knownDomainSet() map[Domain]struct{} {
	domains := reducercontract.KnownDomains()
	set := make(map[Domain]struct{}, len(domains))
	for _, domain := range domains {
		set[domain] = struct{}{}
	}
	return set
}

// AllDomains returns every reducer-owned domain sorted lexicographically: the
// claim/materialization domains in knownDomains plus the shared/edge projection
// domains in allProjectionDomains. It is the single source of truth for tooling
// that must enumerate the full domain set (the capability surface inventory and
// its drift gate), so a domain added to either registry automatically appears in
// the inventory and cannot drain truth without being tracked. Duplicates across
// the two registries collapse to one entry.
func AllDomains() []Domain {
	set := make(map[Domain]struct{}, len(knownDomains)+len(allProjectionDomains))
	for domain := range knownDomains {
		set[domain] = struct{}{}
	}
	for _, domain := range allProjectionDomains {
		set[domain] = struct{}{}
	}
	domains := make([]Domain, 0, len(set))
	for domain := range set {
		domains = append(domains, domain)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i] < domains[j] })
	return domains
}

// ParseDomain converts one raw string into a known reducer domain.
func ParseDomain(raw string) (Domain, error) {
	return reducercontract.ParseDomain(raw)
}
