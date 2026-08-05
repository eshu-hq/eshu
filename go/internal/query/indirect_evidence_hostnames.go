// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
)

const indirectEvidenceHostnameLimit = 4

var genericServiceHostnameTokens = map[string]struct{}{
	"api":     {},
	"app":     {},
	"apps":    {},
	"dev":     {},
	"http":    {},
	"https":   {},
	"node":    {},
	"prod":    {},
	"qa":      {},
	"server":  {},
	"service": {},
	"stage":   {},
	"staging": {},
	"svc":     {},
	"test":    {},
	"web":     {},
}

// boundedIndirectEvidenceHostnamesForService chooses the hostnames most likely
// to identify the service itself before spending cross-repo content searches.
// When no hostname matches a distinctive service token, it preserves the older
// first-four fallback so services with vanity or opaque domains still get
// bounded consumer evidence.
//
// #5720 round-7 P1-3: the returned bool reports that hostnames were dropped
// here. Every drop in this function sits UPSTREAM of every consumer search, so
// a consumer repository reachable only through a dropped hostname never enters
// the merged consumer set at all -- neither the caller's own final cap nor the
// provisioning-candidate read's truncated bool can see it, and the answer would
// otherwise report a complete-looking consumer_repository_count. Only a signal
// returned from here makes those bounds observable.
//
// #5720 round-8 P1-2: this function has TWO drop paths, and until round 8 the
// returned bool covered only one of them. Both are folded in now:
//
//   - the affinity narrowing below, which discards every hostname that carries
//     no distinctive token from the service's own name. A service reached
//     through a legacy or vanity domain (orders-api answering on
//     legacy-billing.acme.test) loses that domain here.
//   - indirectEvidenceHostnameLimit (4), applied by
//     capAndSortIndirectEvidenceHostnames on whichever list survives.
func boundedIndirectEvidenceHostnamesForService(hostnames []string, serviceName string) ([]string, bool) {
	unique := uniqueTrimmedHostnames(hostnames)
	if len(unique) == 0 {
		return nil, false
	}

	tokens := serviceHostnameAffinityTokens(serviceName)
	if len(tokens) > 0 {
		affine := make([]string, 0, len(unique))
		for _, hostname := range unique {
			if hostnameMatchesServiceToken(hostname, tokens) {
				affine = append(affine, hostname)
			}
		}
		if len(affine) > 0 {
			bounded, capped := capAndSortIndirectEvidenceHostnames(affine)
			return bounded, capped || len(affine) < len(unique)
		}
	}

	return capAndSortIndirectEvidenceHostnames(unique)
}

func uniqueTrimmedHostnames(hostnames []string) []string {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		if _, ok := seen[hostname]; ok {
			continue
		}
		seen[hostname] = struct{}{}
		unique = append(unique, hostname)
	}
	return unique
}

func serviceHostnameAffinityTokens(serviceName string) []string {
	rawTokens := strings.FieldsFunc(strings.ToLower(serviceName), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	tokens := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		if len(token) < 4 {
			continue
		}
		if _, generic := genericServiceHostnameTokens[token]; generic {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func hostnameMatchesServiceToken(hostname string, tokens []string) bool {
	hostname = strings.ToLower(hostname)
	for _, token := range tokens {
		if strings.Contains(hostname, token) {
			return true
		}
	}
	return false
}

// capAndSortIndirectEvidenceHostnames returns the bounded, sorted hostname
// list and whether indirectEvidenceHostnameLimit actually dropped entries.
func capAndSortIndirectEvidenceHostnames(hostnames []string) ([]string, bool) {
	truncated := len(hostnames) > indirectEvidenceHostnameLimit
	if truncated {
		hostnames = hostnames[:indirectEvidenceHostnameLimit]
	}
	result := append([]string(nil), hostnames...)
	sort.Strings(result)
	return result, truncated
}
