// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// ServiceCatalogEntityFactKind identifies one provider-native catalog entity.
	ServiceCatalogEntityFactKind = "service_catalog.entity"
	// ServiceCatalogOwnershipFactKind identifies one catalog ownership claim.
	ServiceCatalogOwnershipFactKind = "service_catalog.ownership"
	// ServiceCatalogRepositoryLinkFactKind identifies one declared source
	// repository link from catalog metadata.
	ServiceCatalogRepositoryLinkFactKind = "service_catalog.repository_link"
	// ServiceCatalogDependencyFactKind identifies one declared catalog
	// dependency or relationship.
	ServiceCatalogDependencyFactKind = "service_catalog.dependency"
	// ServiceCatalogAPILinkFactKind identifies one provided or consumed API
	// declaration from catalog metadata.
	ServiceCatalogAPILinkFactKind = "service_catalog.api_link"
	// ServiceCatalogOperationalLinkFactKind identifies one operational link
	// such as docs, runbooks, dashboards, or on-call references.
	ServiceCatalogOperationalLinkFactKind = "service_catalog.operational_link"
	// ServiceCatalogScorecardDefinitionFactKind identifies one scorecard,
	// rubric, rule, or check definition.
	ServiceCatalogScorecardDefinitionFactKind = "service_catalog.scorecard_definition"
	// ServiceCatalogScorecardResultFactKind identifies one entity scorecard or
	// check result.
	ServiceCatalogScorecardResultFactKind = "service_catalog.scorecard_result"
	// ServiceCatalogWarningFactKind identifies non-fatal service-catalog
	// collection warnings.
	ServiceCatalogWarningFactKind = "service_catalog.warning"

	// ServiceCatalogSchemaVersionV1 is the first service-catalog fact schema.
	ServiceCatalogSchemaVersionV1 = "1.0.0"
)

var serviceCatalogFactKinds = []string{
	ServiceCatalogEntityFactKind,
	ServiceCatalogOwnershipFactKind,
	ServiceCatalogRepositoryLinkFactKind,
	ServiceCatalogDependencyFactKind,
	ServiceCatalogAPILinkFactKind,
	ServiceCatalogOperationalLinkFactKind,
	ServiceCatalogScorecardDefinitionFactKind,
	ServiceCatalogScorecardResultFactKind,
	ServiceCatalogWarningFactKind,
}

var serviceCatalogSchemaVersions = map[string]string{
	ServiceCatalogEntityFactKind:              ServiceCatalogSchemaVersionV1,
	ServiceCatalogOwnershipFactKind:           ServiceCatalogSchemaVersionV1,
	ServiceCatalogRepositoryLinkFactKind:      ServiceCatalogSchemaVersionV1,
	ServiceCatalogDependencyFactKind:          ServiceCatalogSchemaVersionV1,
	ServiceCatalogAPILinkFactKind:             ServiceCatalogSchemaVersionV1,
	ServiceCatalogOperationalLinkFactKind:     ServiceCatalogSchemaVersionV1,
	ServiceCatalogScorecardDefinitionFactKind: ServiceCatalogSchemaVersionV1,
	ServiceCatalogScorecardResultFactKind:     ServiceCatalogSchemaVersionV1,
	ServiceCatalogWarningFactKind:             ServiceCatalogSchemaVersionV1,
}

// ServiceCatalogFactKinds returns the accepted service-catalog fact kinds in
// their emission order.
func ServiceCatalogFactKinds() []string {
	return slices.Clone(serviceCatalogFactKinds)
}

// ServiceCatalogSchemaVersion returns the schema version for a service-catalog
// fact kind.
func ServiceCatalogSchemaVersion(factKind string) (string, bool) {
	version, ok := serviceCatalogSchemaVersions[factKind]
	return version, ok
}

// ServiceCatalogFamilyDeclaration is the typed fact-family contract declaration for the
// service_catalog family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var ServiceCatalogFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "service_catalog",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "service_catalog_correlation",
	ProjectionHook:         "service_catalog_correlation",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/service-catalog/correlations",
	TruthProfile:           "deterministic",
	ProviderKeyIndependent: true,
	Kinds: []contract.Kind{
		{Kind: "service_catalog.api_link", Disposition: "unconsumed", NoReducerReason: "No decode-side consumer today; the service-catalog correlation decodes only entity, ownership, repository_link, and operational_link.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries)"},
		{Kind: "service_catalog.dependency", Disposition: "unconsumed", NoReducerReason: "No decode-side consumer today; the service-catalog correlation decodes only entity, ownership, repository_link, and operational_link.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries)"},
		{Kind: "service_catalog.entity", PayloadSchema: "sdk/go/factschema/schema/service_catalog.entity.v1.schema.json"},
		{Kind: "service_catalog.operational_link", PayloadSchema: "sdk/go/factschema/schema/service_catalog.operational_link.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/incident/store"},
		{Kind: "service_catalog.ownership", PayloadSchema: "sdk/go/factschema/schema/service_catalog.ownership.v1.schema.json"},
		{Kind: "service_catalog.repository_link", PayloadSchema: "sdk/go/factschema/schema/service_catalog.repository_link.v1.schema.json"},
		{Kind: "service_catalog.scorecard_definition", Disposition: "unconsumed", NoReducerReason: "No decode-side consumer today; the service-catalog correlation decodes only entity, ownership, repository_link, and operational_link.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries)"},
		{Kind: "service_catalog.scorecard_result", Disposition: "unconsumed", NoReducerReason: "No decode-side consumer today; the service-catalog correlation decodes only entity, ownership, repository_link, and operational_link.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries)"},
		{Kind: "service_catalog.warning", Disposition: "unconsumed", NoReducerReason: "No decode-side consumer today; the service-catalog correlation decodes only entity, ownership, repository_link, and operational_link.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries)"},
	},
	Producers: []contract.Producer{
		{ID: "git-service-catalog-core", Repository: "eshu-hq/eshu", Path: "go/internal/collector/servicecatalog", AuthorityMode: "core_owned", Kinds: []string{"service_catalog.api_link", "service_catalog.dependency", "service_catalog.entity", "service_catalog.operational_link", "service_catalog.ownership", "service_catalog.repository_link", "service_catalog.scorecard_definition", "service_catalog.scorecard_result", "service_catalog.warning"}},
	},
})
