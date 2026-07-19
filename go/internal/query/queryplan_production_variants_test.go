// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

const handlerQueryplanRegisteredCloudResourceListVariant = "cloud-resource-list/resource-type"

const (
	handlerQueryplanDeferredGlobalVariantIssue = 5318
	handlerQueryplanDeferredGlobalFamilySHA256 = "18365ef4e603f391f34e1d9e788d3cb2fb861588e8245a42101a28b4a233ff14"
	handlerQueryplanSafeVariantFamilySHA256    = "b01b91652a1bd8146b9ca4a3e1d4e4e4001ba72bbc05697e943e182e57f1fa15"
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
	wantSafe := 13 + len(allInfraLabels)*10 + 31
	if got := len(safe); got != wantSafe {
		t.Fatalf("safe production variant count = %d, want %d", got, wantSafe)
	}
	if got := queryplan.ProductionCypherFamilySHA256(safe); got != handlerQueryplanSafeVariantFamilySHA256 {
		t.Fatalf("safe production variant family SHA-256 = %s, want %s; live PROFILE coverage must change with the family", got, handlerQueryplanSafeVariantFamilySHA256)
	}
}

func TestHandlerQueryplanCloudResourceListVariantsStayCovered(t *testing.T) {
	t.Parallel()

	expected := cloudResourceListQueryplanVariants()
	if got, want := len(expected), 32; got != want {
		t.Fatalf("cloud resource list reachable variant count = %d, want %d", got, want)
	}
	covered := map[string]string{
		handlerQueryplanRegisteredCloudResourceListVariant: handlerQueryplanProductionCypher()["QP-CLOUD-RESOURCE-LIST"],
	}
	for name, cypher := range handlerQueryplanSafeCypherVariants() {
		if strings.HasPrefix(name, "cloud-resource-list/") {
			covered[name] = cypher
		}
	}
	for name, cypher := range expected {
		if covered[name] != cypher {
			t.Errorf("cloud resource list variant %q is not bound to its production query", name)
		}
	}
}

func cloudResourceListQueryplanVariants() map[string]string {
	variants := make(map[string]string, 32)
	for mask := 0; mask < 32; mask++ {
		filter := cloudResourceListFilter{}
		cursor := cloudResourceListCursor{}
		parts := make([]string, 0, 5)
		if mask&1 != 0 {
			filter.Provider = "proof-provider"
			parts = append(parts, "provider")
		}
		if mask&2 != 0 {
			filter.ResourceType = "proof-type"
			parts = append(parts, "resource-type")
		}
		if mask&4 != 0 {
			filter.Region = "proof-region"
			parts = append(parts, "region")
		}
		if mask&8 != 0 {
			filter.AccountID = "proof-account"
			parts = append(parts, "account")
		}
		if mask&16 != 0 {
			cursor = cloudResourceListCursor{AfterResourceType: "proof-type", AfterID: "proof-id"}
			parts = append(parts, "cursor")
		}
		if len(parts) == 0 {
			parts = append(parts, "unfiltered")
		}
		cypher, _ := buildCloudResourceListQuery(filter, cursor, 10)
		variants["cloud-resource-list/"+strings.Join(parts, "+")] = cypher
	}
	return variants
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
	variants := make(map[string]string, 13+len(allInfraLabels)*10+31)

	for name, cypher := range cloudResourceListQueryplanVariants() {
		if name == handlerQueryplanRegisteredCloudResourceListVariant {
			continue
		}
		variants[name] = cypher
	}

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
