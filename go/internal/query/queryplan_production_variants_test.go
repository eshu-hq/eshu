// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

const (
	handlerQueryplanDeferredGlobalVariantIssue = 5318
	handlerQueryplanDeferredGlobalFamilySHA256 = "18365ef4e603f391f34e1d9e788d3cb2fb861588e8245a42101a28b4a233ff14"
	handlerQueryplanSafeVariantFamilySHA256    = "3780d95079cca1274615b7814159de40b894e8ca8424286e39b6937fc0b53fcb"
)

func TestHandlerQueryplanProductionVariantFamiliesStayExplicit(t *testing.T) {
	t.Parallel()

	deferred := handlerQueryplanDeferredGlobalCypherVariants()
	if got, want := len(deferred), 18; got != want {
		t.Fatalf("deferred global variant count = %d, want %d", got, want)
	}
	if got := queryplan.ProductionCypherFamilySHA256(deferred); got != handlerQueryplanDeferredGlobalFamilySHA256 {
		t.Fatalf(
			"deferred global variant family SHA-256 = %s, want %s; audit every changed branch under #%d",
			got,
			handlerQueryplanDeferredGlobalFamilySHA256,
			handlerQueryplanDeferredGlobalVariantIssue,
		)
	}

	safe := handlerQueryplanSafeCypherVariants()
	wantSafe := 13 + len(allInfraLabels)*10
	if got := len(safe); got != wantSafe {
		t.Fatalf("safe production variant count = %d, want %d", got, wantSafe)
	}
	if got := queryplan.ProductionCypherFamilySHA256(safe); got != handlerQueryplanSafeVariantFamilySHA256 {
		t.Fatalf("safe production variant family SHA-256 = %s, want %s; live PROFILE coverage must change with the family", got, handlerQueryplanSafeVariantFamilySHA256)
	}
}

func handlerQueryplanDeferredGlobalCypherVariants() map[string]string {
	allAccess := repositoryAccessFilter{allScopes: true}
	scopedAccess := queryplanScopedRepositoryAccess()
	variants := make(map[string]string, 18)
	entityTypes := []struct {
		name string
		kind string
	}{
		{name: "untyped"},
		{name: "label", kind: "function"},
		{name: "semantic-kind", kind: "guard"},
		{name: "module-kind", kind: "protocol_implementation"},
		{name: "attribute-kind", kind: "module_attribute"},
	}
	for _, access := range []struct {
		name   string
		filter repositoryAccessFilter
	}{
		{name: "all", filter: allAccess},
		{name: "scoped", filter: scopedAccess},
	} {
		for _, entityType := range entityTypes {
			cypher, _ := buildResolveEntityGraphQuery(resolveEntityRequest{
				Name: "proof",
				Type: entityType.kind,
			}, 10, access.filter)
			variants["entity/"+access.name+"/"+entityType.name] = cypher
		}
		for _, exact := range []bool{false, true} {
			for _, language := range []string{"", "go"} {
				matchName := "contains"
				if exact {
					matchName = "exact"
				}
				languageName := "any-language"
				if language != "" {
					languageName = "language"
				}
				cypher, _ := buildSearchGraphEntitiesQuery("", "proof", language, 10, exact, access.filter)
				variants[fmt.Sprintf("code/%s/%s/%s", access.name, matchName, languageName)] = cypher
			}
		}
	}
	return variants
}

func handlerQueryplanSafeCypherVariants() map[string]string {
	allAccess := repositoryAccessFilter{allScopes: true}
	scopedAccess := queryplanScopedRepositoryAccess()
	variants := make(map[string]string, 13+len(allInfraLabels)*10)

	for _, entityType := range []struct {
		name string
		kind string
	}{
		{name: "untyped"},
		{name: "label", kind: "function"},
		{name: "semantic-kind", kind: "guard"},
		{name: "module-kind", kind: "protocol_implementation"},
		{name: "attribute-kind", kind: "module_attribute"},
	} {
		cypher, _ := buildResolveEntityGraphQuery(resolveEntityRequest{
			Name:   "proof",
			Type:   entityType.kind,
			RepoID: "proof-repository",
		}, 10, allAccess)
		variants["entity/repository/"+entityType.name] = cypher
	}
	for _, exact := range []bool{false, true} {
		for _, language := range []string{"", "go"} {
			matchName := "contains"
			if exact {
				matchName = "exact"
			}
			languageName := "any-language"
			if language != "" {
				languageName = "language"
			}
			cypher, _ := buildSearchGraphEntitiesQuery("proof-repository", "proof", language, 10, exact, allAccess)
			variants[fmt.Sprintf("code/repository/%s/%s", matchName, languageName)] = cypher
		}
	}
	for _, access := range []struct {
		name   string
		filter repositoryAccessFilter
	}{
		{name: "all", filter: allAccess},
		{name: "scoped", filter: scopedAccess},
	} {
		property, relationship, _ := buildResolveWorkloadQueries("proof", "", 10, access.filter)
		variants["workload/"+access.name+"/property"] = property
		variants["workload/"+access.name+"/relationship"] = relationship
	}
	for _, label := range allInfraLabels {
		for _, withARN := range []bool{false, true} {
			identityName := "resource-id"
			selected := &resourceInvestigationCandidate{
				ID:     "proof-resource",
				Labels: []string{label},
			}
			if withARN {
				identityName = "arn"
				selected.Arn = "arn:proof"
			}
			prefix := fmt.Sprintf("resource/%s/%s", label, identityName)
			variants[prefix+"/workloads"] = resourceInvestigationWorkloadsCypher(selected)
			for _, depth := range []int{1, resourceInvestigationMaxDepth} {
				for _, direction := range []string{"incoming", "outgoing"} {
					variants[fmt.Sprintf("%s/paths/%s/depth-%d", prefix, direction, depth)] = resourceInvestigationRepoPathsCypher(
						resourceInvestigationRequest{MaxDepth: depth, Limit: 10},
						selected,
						direction,
					)
				}
			}
		}
	}
	return variants
}

func queryplanScopedRepositoryAccess() repositoryAccessFilter {
	return repositoryAccessFilter{
		allowedScopeIDs:      []string{"proof-scope"},
		allowedRepositoryIDs: []string{"proof-repository"},
		allowed:              map[string]struct{}{"proof-scope": {}, "proof-repository": {}},
	}
}
