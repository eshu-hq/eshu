// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datazone

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon DataZone governance observations for one
// AWS claim. Implementations read control-plane metadata through the DataZone
// management APIs and never read or persist business glossaries, glossary terms,
// catalog asset content, subscription data, or any data-plane payload.
type Client interface {
	// Snapshot returns every DataZone domain visible to the configured AWS
	// credentials, each carrying its projects, environments, and data sources.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures DataZone domain metadata plus non-fatal scan warnings.
type Snapshot struct {
	// Domains is the metadata-only set of DataZone domains, each carrying its
	// projects, environments, and data sources.
	Domains []Domain
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Domain is the scanner-owned Amazon DataZone domain model. It carries
// control-plane metadata only and intentionally excludes glossaries, glossary
// terms, catalog asset content, and subscription data.
type Domain struct {
	// ARN is the Amazon Resource Name that uniquely identifies the domain.
	ARN string
	// ID is the DataZone domain identifier (for example dzd_xxxxxxxx). It is the
	// resource_id the domain node publishes and the key child resources reference.
	ID string
	// Name is the DataZone domain name.
	Name string
	// Status is the domain lifecycle status (for example AVAILABLE).
	Status string
	// Description is the optional domain description.
	Description string
	// KMSKeyIdentifier is the KMS key DataZone reports for domain encryption. It
	// may be a key id, key ARN, or alias as AWS returns it.
	KMSKeyIdentifier string
	// DomainExecutionRole is the IAM role ARN DataZone assumes for domain
	// execution, when reported.
	DomainExecutionRole string
	// ServiceRole is the IAM service-role ARN DataZone uses, when reported.
	ServiceRole string
	// PortalURL is the domain data-portal URL, when reported.
	PortalURL string
	// CreatedAt is when the domain was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the domain was last updated.
	LastUpdatedAt time.Time
	// Tags carries the domain resource tags.
	Tags map[string]string
	// Projects are the metadata-only projects that live under this domain.
	Projects []Project
	// Environments are the metadata-only environments that live under this
	// domain.
	Environments []Environment
	// DataSources are the metadata-only data sources that live under this domain.
	DataSources []DataSource
}

// Project is the scanner-owned Amazon DataZone project model. It carries
// control-plane metadata only.
type Project struct {
	// ID is the DataZone project identifier.
	ID string
	// DomainID is the identifier of the parent DataZone domain.
	DomainID string
	// Name is the project name.
	Name string
	// Description is the optional project description.
	Description string
	// Status is the project lifecycle status.
	Status string
	// ProjectCategory is the reported project category, when present.
	ProjectCategory string
	// DomainUnitID is the identifier of the owning domain unit, when present.
	DomainUnitID string
	// CreatedAt is when the project was created.
	CreatedAt time.Time
	// UpdatedAt is when the project was last updated.
	UpdatedAt time.Time
}

// Environment is the scanner-owned Amazon DataZone environment model. It carries
// control-plane metadata only.
type Environment struct {
	// ID is the DataZone environment identifier.
	ID string
	// DomainID is the identifier of the parent DataZone domain.
	DomainID string
	// ProjectID is the identifier of the parent DataZone project.
	ProjectID string
	// Name is the environment name.
	Name string
	// Description is the optional environment description.
	Description string
	// Provider is the environment provider (for example Amazon DataZone).
	Provider string
	// Status is the environment lifecycle status.
	Status string
	// BlueprintID is the environment blueprint identifier, when present.
	BlueprintID string
	// ProfileID is the environment profile identifier, when present.
	ProfileID string
	// AWSAccountID is the AWS account the environment is provisioned in, when
	// reported.
	AWSAccountID string
	// AWSAccountRegion is the AWS Region the environment is provisioned in, when
	// reported.
	AWSAccountRegion string
	// CreatedAt is when the environment was created.
	CreatedAt time.Time
	// UpdatedAt is when the environment was last updated.
	UpdatedAt time.Time
}

// DataSource is the scanner-owned Amazon DataZone data source model. It carries
// control-plane metadata only and intentionally excludes ingested asset
// content, relational filter expressions, and access credentials. The backing
// identifiers are limited to the names DataZone reports for the source store so
// the scanner can join them to scanned Glue/Redshift resources.
type DataSource struct {
	// ID is the DataZone data source identifier.
	ID string
	// DomainID is the identifier of the parent DataZone domain.
	DomainID string
	// ProjectID is the identifier of the parent DataZone project.
	ProjectID string
	// EnvironmentID is the identifier of the parent DataZone environment, when
	// reported.
	EnvironmentID string
	// Name is the data source name.
	Name string
	// Description is the optional data source description.
	Description string
	// Type is the data source type DataZone reports (for example GLUE,
	// REDSHIFT).
	Type string
	// Status is the data source lifecycle status.
	Status string
	// Enabled reports whether the data source is enabled.
	Enabled bool
	// ConnectionID is the connection identifier the data source uses, when
	// reported.
	ConnectionID string
	// GlueDatabaseNames are the AWS Glue Data Catalog database names this data
	// source ingests from, derived from the Glue run configuration relational
	// filters. They key the backs-Glue-database edge. Filter expressions are
	// intentionally excluded.
	GlueDatabaseNames []string
	// RedshiftClusterName is the provisioned Amazon Redshift cluster name backing
	// this data source, when the source is a provisioned-cluster Redshift source.
	// It keys the backs-Redshift-cluster edge. Serverless workgroups are not
	// resolvable to a published node id and are intentionally omitted.
	RedshiftClusterName string
	// BackingAccountID is the AWS account that owns the backing store, when the
	// data source configuration reports it; it scopes the synthesized Redshift
	// cluster ARN. Empty means the scan boundary account.
	BackingAccountID string
	// BackingRegion is the AWS Region of the backing store, when the data source
	// configuration reports it; it scopes the synthesized Redshift cluster ARN.
	// Empty means the scan boundary region.
	BackingRegion string
	// CreatedAt is when the data source was created.
	CreatedAt time.Time
	// UpdatedAt is when the data source was last updated.
	UpdatedAt time.Time
}
