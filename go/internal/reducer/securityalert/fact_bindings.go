// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// PackageRegistryHandleBinding declares this package's verified reducer consumption of
// package_registry kinds (issue #6629). Build-time metadata only.
var PackageRegistryHandleBinding = contract.Bind(contract.Binding{
	ID: "securityalert/securityalert_handle_package_registry", FamilyID: "package_registry",
	ReducerDir: "go/internal/reducer/securityalert", Symbol: "Handle",
	Kinds: []string{"package_registry.package"},
})

// ReducerDerivedBuildSecurityAlertReconciliationsBinding declares this package's verified reducer consumption of
// reducer_derived kinds (issue #6629). Build-time metadata only.
var ReducerDerivedBuildSecurityAlertReconciliationsBinding = contract.Bind(contract.Binding{
	ID: "securityalert/securityalert_build_security_alert_reconciliations", FamilyID: "reducer_derived",
	ReducerDir: "go/internal/reducer/securityalert", Symbol: "BuildSecurityAlertReconciliations",
	Kinds: []string{"reducer_package_consumption_correlation", "reducer_supply_chain_impact_finding"},
})

// SecurityAlertHandleBinding declares this package's verified reducer consumption of
// security_alert kinds (issue #6629). Build-time metadata only.
var SecurityAlertHandleBinding = contract.Bind(contract.Binding{
	ID: "securityalert/securityalert_handle_security_alert", FamilyID: "security_alert",
	ReducerDir: "go/internal/reducer/securityalert", Symbol: "Handle",
	Kinds: []string{"security_alert.repository_alert"},
})
