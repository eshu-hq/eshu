// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDatazone identifies the regional Amazon DataZone metadata-only scan
	// slice. The scanner reads governance control-plane metadata through the
	// DataZone management APIs (ListDomains, GetDomain, ListProjects,
	// ListEnvironments, ListDataSources, GetDataSource, ListTagsForResource) and
	// never reads or persists business glossaries, glossary terms, catalog asset
	// content, subscription data, or any data-plane payload, and never mutates
	// DataZone state.
	ServiceDatazone = "datazone"
)

const (
	// ResourceTypeDatazoneDomain identifies an Amazon DataZone domain metadata
	// resource. The scanner emits identity, status, the encryption KMS key
	// reference, the domain execution / service IAM role references, and
	// lifecycle timestamps only.
	ResourceTypeDatazoneDomain = "aws_datazone_domain"
	// ResourceTypeDatazoneProject identifies an Amazon DataZone project metadata
	// resource. The scanner emits identity, parent domain, status, and category
	// only.
	ResourceTypeDatazoneProject = "aws_datazone_project"
	// ResourceTypeDatazoneEnvironment identifies an Amazon DataZone environment
	// metadata resource. The scanner emits identity, parent domain and project,
	// provider, blueprint/profile identifiers, and the target AWS account/region
	// only.
	ResourceTypeDatazoneEnvironment = "aws_datazone_environment"
	// ResourceTypeDatazoneDataSource identifies an Amazon DataZone data source
	// metadata resource. The scanner emits identity, parent domain/project/
	// environment, source type, and enablement only. Ingested asset content,
	// filter expressions, and access credentials stay outside the contract.
	ResourceTypeDatazoneDataSource = "aws_datazone_data_source"
)

const (
	// RelationshipDatazoneDomainUsesKMSKey records a DataZone domain's reported
	// KMS encryption key dependency. The target is keyed by the KMS key
	// identifier DataZone reports (key id, key ARN, or alias), matching how the
	// KMS scanner publishes its key resource_id.
	RelationshipDatazoneDomainUsesKMSKey = "datazone_domain_uses_kms_key"
	// RelationshipDatazoneDomainUsesIAMRole records a DataZone domain's reported
	// IAM execution or service role dependency. The target is keyed by the role
	// ARN, matching how the IAM scanner publishes its role resource_id.
	RelationshipDatazoneDomainUsesIAMRole = "datazone_domain_uses_iam_role"
	// RelationshipDatazoneProjectInDomain records a DataZone project's membership
	// in its parent domain. The target is keyed by the domain id the domain node
	// publishes.
	RelationshipDatazoneProjectInDomain = "datazone_project_in_domain"
	// RelationshipDatazoneEnvironmentInDomain records a DataZone environment's
	// membership in its parent domain. The target is keyed by the domain id the
	// domain node publishes.
	RelationshipDatazoneEnvironmentInDomain = "datazone_environment_in_domain"
	// RelationshipDatazoneDataSourceInDomain records a DataZone data source's
	// membership in its parent domain. The target is keyed by the domain id the
	// domain node publishes.
	RelationshipDatazoneDataSourceInDomain = "datazone_data_source_in_domain"
	// RelationshipDatazoneDataSourceBacksGlueDatabase records a DataZone Glue data
	// source's backing AWS Glue Data Catalog database. The target is keyed by the
	// Glue database name, matching how the Glue scanner publishes its database
	// resource_id.
	RelationshipDatazoneDataSourceBacksGlueDatabase = "datazone_data_source_backs_glue_database"
	// RelationshipDatazoneDataSourceBacksRedshiftCluster records a DataZone
	// Redshift data source's backing provisioned Amazon Redshift cluster. The
	// target is keyed by the partition-aware cluster ARN the Redshift scanner
	// synthesizes and publishes for a cluster node.
	RelationshipDatazoneDataSourceBacksRedshiftCluster = "datazone_data_source_backs_redshift_cluster"
)
