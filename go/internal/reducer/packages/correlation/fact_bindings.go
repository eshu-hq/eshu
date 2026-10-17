// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// CodeBuildPackageSourceDecisionsBinding declares this package's verified reducer consumption of
// code kinds (issue #6629). Build-time metadata only.
var CodeBuildPackageSourceDecisionsBinding = contract.Bind(contract.Binding{
	ID: "correlation/correlation_build_package_source_decisions", FamilyID: "code",
	ReducerDir: "go/internal/reducer/packages/correlation", Symbol: "BuildPackageSourceDecisions",
	Kinds: []string{"repository"},
})

// PackageRegistryHandleBinding declares this package's verified reducer consumption of
// package_registry kinds (issue #6629). Build-time metadata only.
var PackageRegistryHandleBinding = contract.Bind(contract.Binding{
	ID: "correlation/correlation_handle", FamilyID: "package_registry",
	ReducerDir: "go/internal/reducer/packages/correlation", Symbol: "Handle",
	Kinds: []string{"package_registry.package", "package_registry.package_version", "package_registry.source_hint"},
})
