// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

const (
	handlerQueryplanSafeVariantFamilySHA256       = "e208c54ce65fc4bdf5889d434525b44379dc00db15cccc234ce31712eee31a25"
	cloudResourcePageQueryplanFamilySHA256        = "712236c6413a22d03897649a0ac0a58115531537557d9bb3fed5604acd23f2b2"
	entityNameSearchQueryplanVariantFamilySHA256  = "4d4f47c1555b8a42caa91d20a5971902fc19b6ef65d3c77440f9be5df4333ef5"
	entityNameSearchQueryplanBuilderSourceSHA256  = "3057a508e8b5acf4e07b4d5567b00dbf5e900b360eed82b3102f358e2a1e1523"
	entityNameSearchQueryplanExpectedVariantCount = 17
	resourceSelectorQueryplanExpectedVariantCount = 304
)

func TestHandlerQueryplanProductionVariantFamiliesStayExplicit(t *testing.T) {
	t.Parallel()

	safe := handlerQueryplanSafeCypherVariants()
	wantSafe := 13 + len(allInfraLabels)*10 + importDependencyQueryplanExpectedVariantCount + resourceSelectorQueryplanExpectedVariantCount
	if got := len(safe); got != wantSafe {
		t.Fatalf("safe production variant count = %d, want %d", got, wantSafe)
	}
	if got := queryplan.ProductionCypherFamilySHA256(safe); got != handlerQueryplanSafeVariantFamilySHA256 {
		t.Fatalf("safe production variant family SHA-256 = %s, want %s; live PROFILE coverage must change with the family", got, handlerQueryplanSafeVariantFamilySHA256)
	}
}

func TestResourceSelectorQueryplanVariantsCoverEveryReachableShape(t *testing.T) {
	t.Parallel()

	variants := resourceSelectorQueryplanVariants()
	if got, want := len(variants), resourceSelectorQueryplanExpectedVariantCount; got != want {
		t.Fatalf("resource selector variant count = %d, want %d", got, want)
	}
	for name, cypher := range variants {
		if strings.Contains(cypher, "MATCH (n)\n") {
			t.Errorf("resource selector variant %q contains a whole-graph match", name)
		}
		if !strings.HasPrefix(strings.TrimSpace(cypher), "MATCH (n:") {
			t.Errorf("resource selector variant %q is not directly label-anchored", name)
		}
	}
}

func TestHandlerQueryplanCloudResourceListVariantsStayCovered(t *testing.T) {
	t.Parallel()

	variants := cloudResourceListQueryplanVariants()
	if got, want := len(variants), 64; got != want {
		t.Fatalf("cloud resource list reachable variant count = %d, want %d", got, want)
	}
	if got := queryplan.ProductionCypherFamilySHA256(variants); got != cloudResourcePageQueryplanFamilySHA256 {
		t.Fatalf("cloud resource SQL family SHA-256 = %s, want %s; all production variants must remain registered", got, cloudResourcePageQueryplanFamilySHA256)
	}
}

func TestEntityNameSearchQueryplanVariantsStayRegisteredAndSourceBound(t *testing.T) {
	t.Parallel()

	variants := entityNameSearchQueryplanVariants(t)
	if err := validateEntityNameSearchQueryplanVariants(variants); err != nil {
		t.Fatal(err)
	}

	drifted := make(map[string]string, len(variants))
	for name, query := range variants {
		drifted[name] = query
	}
	drifted["entity-name/all/code/exact/any-language"] += " /* unreviewed drift */"
	if err := validateEntityNameSearchQueryplanVariants(drifted); err == nil || !strings.Contains(err.Error(), "family SHA-256") {
		t.Fatalf("drift validation error = %v, want family SHA-256 rejection", err)
	}

	manifest := entityNameSearchQueryplanSourceManifest()
	manifest.Entries[0].Source.SourceSHA256 = strings.Repeat("0", 64)
	if err := queryplan.ValidateManifestSources(manifest, "../../.."); err == nil || !strings.Contains(err.Error(), "source_sha256") {
		t.Fatalf("source drift validation error = %v, want source_sha256 rejection", err)
	}
}

func entityNameSearchQueryplanVariants(t *testing.T) map[string]string {
	t.Helper()

	variants := make(map[string]string, entityNameSearchQueryplanExpectedVariantCount)
	for _, scope := range []struct {
		name          string
		value         EntityNameScope
		repositoryIDs []string
	}{
		{name: "all", value: EntityNameScopeAll},
		{name: "scoped", value: EntityNameScopeRepositories, repositoryIDs: []string{"proof-repository"}},
	} {
		addVariant := func(name string, search EntityNameSearch) {
			search.Name = "proof"
			search.Scope = scope.value
			search.RepositoryIDs = scope.repositoryIDs
			search.Limit = 10
			normalized, empty, err := normalizeEntityNameSearch(search)
			if err != nil || empty {
				t.Fatalf("normalize %s/%s: empty=%v err=%v", scope.name, name, empty, err)
			}
			query, _ := buildEntityNameSearchQuery(normalized)
			variants[fmt.Sprintf("entity-name/%s/%s", scope.name, name)] = query
		}

		for _, entityType := range []struct {
			name          string
			entityType    string
			metadataKey   string
			metadataValue string
		}{
			{name: "entity/label", entityType: "Function"},
			{name: "entity/semantic", entityType: "Function", metadataKey: "semantic_kind", metadataValue: "guard"},
			{name: "entity/module", entityType: "Module", metadataKey: "module_kind", metadataValue: "protocol_implementation"},
			{name: "entity/attribute", entityType: "Variable", metadataKey: "attribute_kind", metadataValue: "module_attribute"},
		} {
			addVariant(entityType.name, EntityNameSearch{
				Match: EntityNameMatchExact, EntityType: entityType.entityType,
				MetadataKey: entityType.metadataKey, MetadataValue: entityType.metadataValue,
			})
		}

		for _, match := range []struct {
			name  string
			value EntityNameMatch
		}{
			{name: "exact", value: EntityNameMatchExact},
			{name: "substring", value: EntityNameMatchSubstring},
		} {
			for _, language := range []struct {
				name  string
				value []string
			}{
				{name: "any-language"},
				{name: "language", value: []string{"go"}},
			} {
				addVariant(fmt.Sprintf("code/%s/%s", match.name, language.name), EntityNameSearch{
					Match: match.value, Languages: language.value,
				})
			}
		}
	}

	_, empty, err := normalizeEntityNameSearch(EntityNameSearch{
		Name: "proof", Match: EntityNameMatchExact, Scope: EntityNameScopeRepositories, Limit: 10,
	})
	if err != nil || !empty {
		t.Fatalf("normalize empty grants: empty=%v err=%v", empty, err)
	}
	variants["entity-name/scoped/empty-grants"] = ""
	return variants
}

func validateEntityNameSearchQueryplanVariants(variants map[string]string) error {
	if got := len(variants); got != entityNameSearchQueryplanExpectedVariantCount {
		return fmt.Errorf("entity-name SQL variant count = %d, want %d", got, entityNameSearchQueryplanExpectedVariantCount)
	}
	if got := queryplan.ProductionCypherFamilySHA256(variants); got != entityNameSearchQueryplanVariantFamilySHA256 {
		return fmt.Errorf("entity-name SQL family SHA-256 = %s, want %s", got, entityNameSearchQueryplanVariantFamilySHA256)
	}
	if err := queryplan.ValidateManifestSources(entityNameSearchQueryplanSourceManifest(), "../../.."); err != nil {
		return fmt.Errorf("entity-name SQL source binding: %w", err)
	}
	return nil
}

func entityNameSearchQueryplanSourceManifest() queryplan.Manifest {
	return queryplan.Manifest{Entries: []queryplan.Entry{{
		ID: "QP-ENTITY-NAME-SEARCH-SQL",
		Source: queryplan.SourceRef{
			File:         "go/internal/query/content_reader_entity_names.go",
			Symbol:       "buildEntityNameSearchQuery",
			SourceSHA256: entityNameSearchQueryplanBuilderSourceSHA256,
		},
	}}}
}

func cloudResourceListQueryplanVariants() map[string]string {
	variants := make(map[string]string, 64)
	for _, access := range []struct {
		name   string
		filter CloudResourceListPageFilter
	}{
		{name: "all", filter: CloudResourceListPageFilter{AllScopes: true}},
		{name: "scoped", filter: CloudResourceListPageFilter{
			AllowedRepositoryIDs: []string{"proof-repository"},
			AllowedScopeIDs:      []string{"proof-scope"},
		}},
	} {
		for mask := 0; mask < 32; mask++ {
			filter := access.filter
			filter.Limit = 10
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
				filter.AfterResourceType = "proof-type"
				filter.AfterID = "proof-id"
				parts = append(parts, "cursor")
			}
			if len(parts) == 0 {
				parts = append(parts, "unfiltered")
			}
			query, _ := buildCloudResourceIdentityListQuery(filter)
			variants["cloud-resource-list/"+access.name+"/"+strings.Join(parts, "+")] = query
		}
	}
	return variants
}

func handlerQueryplanSafeCypherVariants() map[string]string {
	allAccess := repositoryAccessFilter{allScopes: true}
	scopedAccess := queryplanScopedRepositoryAccess()
	variants := make(map[string]string, 13+len(allInfraLabels)*10+importDependencyQueryplanExpectedVariantCount+resourceSelectorQueryplanExpectedVariantCount)

	for name, cypher := range importDependencyQueryplanVariants() {
		variants[name] = cypher
	}

	for name, cypher := range resourceSelectorQueryplanVariants() {
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

func resourceSelectorQueryplanVariants() map[string]string {
	variants := make(map[string]string, resourceSelectorQueryplanExpectedVariantCount)
	accesses := []struct {
		name   string
		filter repositoryAccessFilter
	}{
		{name: "all", filter: repositoryAccessFilter{allScopes: true}},
		{name: "scoped", filter: queryplanScopedRepositoryAccess()},
	}
	shapes := []struct {
		name         string
		resourceType string
	}{
		{name: "default"},
		{name: "queue", resourceType: "queue"},
		{name: "database", resourceType: "database"},
		{name: "cloud", resourceType: "cloud"},
		{name: "k8s", resourceType: "k8s"},
		{name: "terraform", resourceType: "terraform"},
		{name: "module", resourceType: "module"},
	}
	phases := []struct {
		phase      string
		predicates []string
	}{
		{phase: "exact", predicates: resourceInvestigationExactSelectorPredicates},
		{phase: "fuzzy", predicates: resourceInvestigationFuzzySelectorPredicates},
	}
	for _, access := range accesses {
		for _, shape := range shapes {
			for _, environment := range []struct {
				name  string
				value string
			}{
				{name: "any-environment"},
				{name: "environment", value: "proof-environment"},
			} {
				for _, phase := range phases {
					for _, label := range resourceInvestigationSelectorLabels(shape.resourceType) {
						req := resourceInvestigationRequest{
							Query:        "proof",
							ResourceType: shape.resourceType,
							Environment:  environment.value,
							Limit:        10,
						}
						name := fmt.Sprintf(
							"resource-selector/%s/%s/%s/%s/label-%s",
							access.name,
							shape.name,
							environment.name,
							phase.phase,
							strings.ToLower(label),
						)
						variants[name] = resourceInvestigationSelectorLabelCypher(
							req,
							access.filter,
							label,
							phase.predicates,
						)
					}
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
