// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import "time"

type ProviderSecurityAlert struct {
	SecurityAlertReconciliationDecision
	updatedAtTime time.Time
}

type SecurityAlertConsumption struct {
	FactID           string
	EvidenceKind     string
	RepositoryID     string
	RepositoryName   string
	PackageID        string
	RelativePath     string
	ObservedAt       time.Time
	DependencyRange  string
	ObservedVersion  string
	InstalledVersion string
	RequestedRange   string
	DependencyPath   []string
	DependencyDepth  int
	DirectDependency *bool
	DependencyScope  string
	Lockfile         bool
}

type SecurityAlertImpact struct {
	FactID       string
	RepositoryID string
	PackageID    string
	CVEID        string
	AdvisoryID   string
	Status       string
}
