// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Managed Grafana workspace observations
// for one AWS claim. Implementations read control-plane metadata through the
// Managed Grafana management APIs and never read dashboards, alert rules, query
// results, or any data-plane payload, and never read or persist SAML/IAM
// Identity Center authentication secrets or workspace API keys.
type Client interface {
	// Snapshot returns every Managed Grafana workspace visible to the configured
	// AWS credentials, each carrying its control-plane metadata.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Managed Grafana workspace metadata plus non-fatal scan
// warnings.
type Snapshot struct {
	// Workspaces is the metadata-only set of Managed Grafana workspaces.
	Workspaces []Workspace
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Workspace is the scanner-owned Managed Grafana workspace model. It carries
// control-plane metadata only and intentionally excludes dashboards, panels,
// alert rules, query results, SAML/IAM Identity Center authentication secrets,
// and workspace API keys.
type Workspace struct {
	// ID is the unique workspace id (for example g-1234567890).
	ID string
	// ARN is the workspace ARN. Managed Grafana does not report an ARN on the
	// workspace description, so the adapter synthesizes a partition-aware ARN.
	ARN string
	// Name is the workspace display name.
	Name string
	// Description is the user-defined workspace description.
	Description string
	// Status is the current workspace lifecycle status (for example ACTIVE).
	Status string
	// GrafanaVersion is the Grafana version the workspace runs.
	GrafanaVersion string
	// Endpoint is the Grafana console URL for the workspace. It is a console
	// address, not a secret.
	Endpoint string
	// AccountAccessType reports whether the workspace can access resources in
	// this account only or across the organization.
	AccountAccessType string
	// PermissionType reports whether data-source permissions are
	// CUSTOMER_MANAGED or SERVICE_MANAGED.
	PermissionType string
	// WorkspaceRoleARN is the IAM role ARN the workspace assumes to read from
	// configured data sources, when present.
	WorkspaceRoleARN string
	// DataSources are the configured AWS data-source type enums (for example
	// PROMETHEUS, CLOUDWATCH). These are enum names, never connection strings or
	// credentials.
	DataSources []string
	// NotificationDestinations are the configured notification-destination type
	// enums (for example SNS). These are enum names, never endpoint secrets.
	NotificationDestinations []string
	// AuthenticationProviders are the configured authentication provider names
	// (for example AWS_SSO, SAML). Only the provider names are recorded; SAML
	// assertions, IAM Identity Center details, and API keys are never read.
	AuthenticationProviders []string
	// SubnetIDs are the bare VPC subnet ids (subnet-...) from the workspace
	// vpcConfiguration, when configured.
	SubnetIDs []string
	// SecurityGroupIDs are the bare VPC security-group ids (sg-...) from the
	// workspace vpcConfiguration, when configured.
	SecurityGroupIDs []string
	// Created is when the workspace was created.
	Created time.Time
	// Modified is when the workspace was last modified.
	Modified time.Time
	// Tags carries the workspace resource tags.
	Tags map[string]string
}
