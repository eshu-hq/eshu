// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

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
