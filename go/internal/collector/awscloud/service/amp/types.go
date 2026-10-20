// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amp

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Managed Service for Prometheus
// observations for one AWS claim. Implementations read control-plane metadata
// through the Prometheus service (aps) list/describe APIs and never read
// ingested time-series samples, query results, alert-manager definitions,
// rule-group definition bodies, or scrape-configuration bodies.
type Client interface {
	// Snapshot returns every AMP workspace visible to the configured AWS
	// credentials, each carrying its rule-groups namespaces (names only), plus
	// the account's managed-collector scrapers.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures AMP workspace, rule-groups namespace, and scraper metadata
// plus non-fatal scan warnings.
type Snapshot struct {
	// Workspaces is the metadata-only set of AMP workspaces, each carrying its
	// rule-groups namespaces.
	Workspaces []Workspace
	// Scrapers is the metadata-only set of AMP managed-collector scrapers in the
	// account. Scrapers are an account-level list, not nested under a workspace,
	// so they reference their destination workspace by ARN.
	Scrapers []Scraper
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Workspace is the scanner-owned AMP workspace model. It carries control-plane
// metadata only and intentionally excludes ingested samples, query results, and
// alert-manager definitions.
type Workspace struct {
	// ARN is the Amazon Resource Name that uniquely identifies the workspace.
	ARN string
	// WorkspaceID is the unique workspace id (for example ws-...).
	WorkspaceID string
	// Alias is the optional, non-unique human-friendly workspace alias.
	Alias string
	// Status is the current workspace lifecycle status code (for example
	// ACTIVE).
	Status string
	// KMSKeyARN is the ARN of the customer-managed KMS key used to encrypt
	// workspace data, when the workspace was created with one. AWS reports a key
	// ARN here.
	KMSKeyARN string
	// CreatedAt is when the workspace was created.
	CreatedAt time.Time
	// Tags carries the workspace resource tags.
	Tags map[string]string
	// RuleGroupsNamespaces are the metadata-only rule-groups namespaces (names
	// only) that live under this workspace.
	RuleGroupsNamespaces []RuleGroupsNamespace
}

// RuleGroupsNamespace is the scanner-owned AMP rule-groups namespace model. It
// carries the namespace NAME and identity only. The recording-rule and
// alerting-rule definition body is intentionally excluded: the scanner never
// reads or persists rule definitions.
type RuleGroupsNamespace struct {
	// ARN is the Amazon Resource Name that uniquely identifies the namespace.
	ARN string
	// Name is the rule-groups namespace name.
	Name string
	// Status is the current namespace lifecycle status code.
	Status string
	// CreatedAt is when the namespace was created.
	CreatedAt time.Time
	// ModifiedAt is when the namespace was last modified.
	ModifiedAt time.Time
	// Tags carries the namespace resource tags.
	Tags map[string]string
}

// Scraper is the scanner-owned AMP managed-collector (scraper) model. It carries
// control-plane metadata only. The scrape-configuration body (which can encode
// relabel rules and target hints) is intentionally excluded: the scanner never
// reads or persists scrape configuration.
type Scraper struct {
	// ARN is the Amazon Resource Name that uniquely identifies the scraper.
	ARN string
	// ScraperID is the unique scraper id (for example s-...).
	ScraperID string
	// Alias is the optional human-friendly scraper alias.
	Alias string
	// Status is the current scraper lifecycle status code (for example ACTIVE).
	Status string
	// RoleARN is the IAM role ARN the scraper assumes to discover and collect
	// metrics, when reported.
	RoleARN string
	// SourceEKSClusterARN is the ARN of the Amazon EKS source cluster the
	// scraper collects metrics from, when the source is an EKS configuration.
	SourceEKSClusterARN string
	// DestinationWorkspaceARN is the ARN of the AMP workspace the scraper sends
	// metrics to.
	DestinationWorkspaceARN string
	// SubnetIDs are the bare EKS VPC configuration subnet ids the scraper
	// reports (subnet-...).
	SubnetIDs []string
	// SecurityGroupIDs are the bare EKS VPC configuration security-group ids the
	// scraper reports (sg-...).
	SecurityGroupIDs []string
	// CreatedAt is when the scraper was created.
	CreatedAt time.Time
	// Tags carries the scraper resource tags.
	Tags map[string]string
}
