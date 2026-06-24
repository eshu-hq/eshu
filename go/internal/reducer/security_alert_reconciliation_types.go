// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "time"

type providerSecurityAlert struct {
	SecurityAlertReconciliationDecision
	updatedAtTime time.Time
}

type securityAlertConsumption struct {
	factID           string
	evidenceKind     string
	repositoryID     string
	repositoryName   string
	packageID        string
	relativePath     string
	observedAt       time.Time
	dependencyRange  string
	observedVersion  string
	installedVersion string
	requestedRange   string
	dependencyPath   []string
	dependencyDepth  int
	directDependency *bool
	dependencyScope  string
	lockfile         bool
}

type securityAlertImpact struct {
	factID       string
	repositoryID string
	packageID    string
	cveID        string
	advisoryID   string
	status       string
}
