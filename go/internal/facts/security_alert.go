// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

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
