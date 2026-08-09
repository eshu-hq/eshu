// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// claimGatedDomainsWithoutAClaimGate lists reducer domains that return a
// readiness-gate miss from their handler but have NO row in
// reducerClaimReadinessRequirementsSQL, so nothing stops the intent being
// claimed before its upstream phase publishes. The handler's own
// ReadinessLookup is their only defense.
//
// That is strictly worse than the two-layer arrangement every other readiness
// domain has, and it is the shape #5047 called "the wide-open case" when GCP
// relationship materialization was in it. Enrolling the class in
// nonCountingReducerRetryFailureClasses (#5046) stops the miss dead-lettering
// the intent, which is an improvement on its own, but it does not add the
// missing claim gate.
//
// Listing the gap here rather than leaving it implicit: adding a CTE row
// changes claim-time behaviour for a domain and needs its own claim-path proof,
// which is a different change from enrolling a failure class.
var claimGatedDomainsWithoutAClaimGate = map[string]string{
	"aws_cloud_image_materialization": "waits on cloud_resource_uid/canonical_nodes_committed " +
		"via AWSCloudImageMaterializationHandler.sourceNodesReady, the same phase and keyspace " +
		"aws_relationship_materialization gates on at claim time",
}

// TestReadinessDomainsWithoutAClaimGateAreTheKnownSet keeps the gap from
// growing quietly.
//
// It compares the domains named in the claim-time readiness CTE against the
// domains whose handlers return an enrolled readiness class, and requires any
// difference to be already listed above with a reason. A new readiness domain
// that lands without a claim gate fails here instead of silently relying on a
// single layer of defense.
func TestReadinessDomainsWithoutAClaimGateAreTheKnownSet(t *testing.T) {
	t.Parallel()

	gated := claimGatedDomains(t)
	if len(gated) == 0 {
		t.Fatal("parsed no domains out of the readiness CTE; the scan is broken, not the CTE")
	}

	var ungated []string
	for class := range readinessFailureClassesInReducer(t) {
		domain := domainForReadinessClass(class)
		if domain == "" || gated[domain] {
			continue
		}
		// A class name is not a domain name. aws_relationship_ec2_instance_
		// nodes_not_ready, for example, is a sub-readiness INSIDE
		// aws_relationship_materialization, not a domain of its own. Validating
		// against the real domain enum keeps the guard from inventing a domain
		// out of a class name and then reporting it as ungated.
		if _, err := reducer.ParseDomain(domain); err != nil {
			continue
		}
		if _, known := claimGatedDomainsWithoutAClaimGate[domain]; known {
			continue
		}
		ungated = append(ungated, domain+" (class "+class+")")
	}
	sort.Strings(ungated)

	if len(ungated) > 0 {
		t.Fatalf(
			"reducer domains returning a readiness-gate miss with NO row in "+
				"reducerClaimReadinessRequirementsSQL:\n  %s\n"+
				"Their handler's ReadinessLookup is the only defense, so an intent can be claimed "+
				"before its upstream phase publishes. Add the claim-time row, or add the domain to "+
				"claimGatedDomainsWithoutAClaimGate with the reason it is acceptable.",
			strings.Join(ungated, "\n  "),
		)
	}

	// The known-gap list must not outlive the gap it documents.
	for domain := range claimGatedDomainsWithoutAClaimGate {
		if gated[domain] {
			t.Errorf(
				"%s is listed in claimGatedDomainsWithoutAClaimGate but now HAS a claim-time row; "+
					"drop the entry so the list keeps meaning what it says",
				domain,
			)
		}
	}
}

func claimGatedDomains(t *testing.T) map[string]bool {
	t.Helper()

	domains := map[string]bool{}
	for _, match := range regexp.MustCompile(`\('([a-z0-9_]+)',`).
		FindAllStringSubmatch(reducerClaimReadinessRequirementsSQL(), -1) {
		domains[match[1]] = true
	}
	return domains
}

// domainForReadinessClass maps a "<domain>_nodes_not_ready" class back to a
// candidate reducer domain name. The caller validates the result against
// reducer.ParseDomain, because several classes name a sub-readiness within a
// domain rather than a domain. Classes that do not follow the convention
// return "" and are skipped rather than guessed at.
func domainForReadinessClass(class string) string {
	trimmed, ok := strings.CutSuffix(class, "_nodes_not_ready")
	if !ok {
		return ""
	}
	return trimmed + "_materialization"
}
