// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// ServiceCatalogHandleBinding declares this package's verified reducer consumption of
// service_catalog kinds (issue #6629). Build-time metadata only.
var ServiceCatalogHandleBinding = contract.Bind(contract.Binding{
	ID: "servicecatalog/servicecatalog_handle", FamilyID: "service_catalog",
	ReducerDir: "go/internal/reducer/servicecatalog", Symbol: "Handle",
	Kinds: []string{"service_catalog.entity", "service_catalog.ownership", "service_catalog.repository_link"},
})
