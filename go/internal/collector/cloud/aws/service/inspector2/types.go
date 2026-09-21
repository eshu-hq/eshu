// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inspector2

import "context"

// Client is the Amazon Inspector v2 metadata read surface consumed by Scanner.
// Runtime adapters translate AWS SDK responses into these scanner-owned
// metadata records. The interface intentionally exposes no finding-body read,
// no filter-criteria read, and no mutation call.
type Client interface {
	// AccountStatus returns the Inspector v2 status and enabled scan features
	// for the claimed account.
	AccountStatus(context.Context) (AccountStatus, error)
	// ListMembers returns the member accounts visible to the claimed account
	// when it is a delegated administrator. It returns an empty slice for a
	// non-administrator account.
	ListMembers(context.Context) ([]MemberAccount, error)
	// ListFilters returns findings filter metadata. Implementations MUST drop
	// filter criteria expressions and other free-text fields.
	ListFilters(context.Context) ([]FilterSummary, error)
	// ListCisScanConfigurations returns CIS scan configuration metadata,
	// including the configured target account set.
	ListCisScanConfigurations(context.Context) ([]CisScanConfiguration, error)
}

// AccountStatus is the scanner-owned view of the Inspector v2 status for one
// account. Features carry per-resource-type enablement only.
type AccountStatus struct {
	AccountID string
	Status    string
	Features  []FeatureStatus
}

// FeatureStatus is the enablement state of one Inspector v2 scan feature
// (EC2, ECR, Lambda, or Lambda code) for an account.
type FeatureStatus struct {
	// Feature is the canonical feature key (ec2, ecr, lambda, lambda_code).
	Feature string
	// Status is the Inspector v2 enablement status string for the feature.
	Status string
}

// MemberAccount is a metadata-only Inspector v2 member account summary as
// reported by a delegated administrator account.
type MemberAccount struct {
	AccountID          string
	AdministratorID    string
	RelationshipStatus string
	UpdatedAt          string
}

// FilterSummary is an Inspector v2 findings filter identity. It deliberately
// omits the filter criteria expression, description, and reason because those
// fields encode threat-hunting hypotheses that Eshu must never persist.
type FilterSummary struct {
	ARN     string
	Name    string
	Action  string
	OwnerID string
}

// CisScanConfiguration is the metadata-only view of an Inspector v2 CIS scan
// configuration, including the target account set but excluding scan results.
type CisScanConfiguration struct {
	ARN            string
	Name           string
	OwnerID        string
	SecurityLevel  string
	ScheduleKind   string
	TargetAccounts []string
	Tags           map[string]string
}
