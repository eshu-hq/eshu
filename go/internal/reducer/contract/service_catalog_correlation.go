// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// ServiceCatalogCorrelationFactKind names the durable fact kind the
// service-catalog-correlation writer publishes under. It is exported so
// families both above and below the reducer root (the still-in-root
// supply_chain_impact family, and internal/reducer/servicecatalog itself) can
// name it without either importing the other, which would violate the
// strictly downward package-import direction (root -> family -> shared-core
// -> contract).
const ServiceCatalogCorrelationFactKind = "reducer_service_catalog_correlation"
