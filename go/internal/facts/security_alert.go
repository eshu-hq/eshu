// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// SecurityAlertRepositoryAlertFactKind identifies one repository-scoped
	// security alert reported by an external provider such as GitHub
	// Dependabot. It is source evidence only.
	SecurityAlertRepositoryAlertFactKind = "security_alert.repository_alert"

	// SecurityAlertSchemaVersionV1 is the first repository security-alert fact
	// schema.
	SecurityAlertSchemaVersionV1 = "1.0.0"
)

var securityAlertFactKinds = []string{
	SecurityAlertRepositoryAlertFactKind,
}

var securityAlertSchemaVersions = map[string]string{
	SecurityAlertRepositoryAlertFactKind: SecurityAlertSchemaVersionV1,
}

// SecurityAlertFactKinds returns the accepted repository security-alert fact
// kinds in source-contract order.
func SecurityAlertFactKinds() []string {
	return slices.Clone(securityAlertFactKinds)
}

// SecurityAlertSchemaVersion returns the schema version for a repository
// security-alert fact kind.
func SecurityAlertSchemaVersion(factKind string) (string, bool) {
	version, ok := securityAlertSchemaVersions[factKind]
	return version, ok
}

// SecurityAlertFamilyDeclaration is the typed fact-family contract declaration for the
// security_alert family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var SecurityAlertFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "security_alert",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "security_alert_reconciliation",
	ProjectionHook:         "security_alert_reconciliation",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/supply-chain/security-alerts/reconciliations",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "security_alert.repository_alert", PayloadSchema: "sdk/go/factschema/schema/security_alert.repository_alert.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "collector-security-alerts", Repository: "eshu-hq/eshu", Path: "go/internal/collector/securityalert", PlannedRepository: "eshu-hq/eshu-collector-security-alerts", AuthorityMode: "core_issued_grant", Kinds: []string{"security_alert.repository_alert"}},
	},
})
