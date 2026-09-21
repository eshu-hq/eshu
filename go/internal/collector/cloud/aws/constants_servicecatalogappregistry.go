// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceServiceCatalogAppRegistry identifies the regional AWS Service
	// Catalog AppRegistry metadata-only scan slice. The scanner reads
	// application and attribute-group control-plane metadata through the
	// AppRegistry list APIs (ListApplications, ListAttributeGroups,
	// ListAssociatedResources, ListAttributeGroupsForApplication,
	// ListTagsForResource) and never reads or persists attribute-group content
	// bodies, never reads associated-resource tag values, and never mutates
	// AppRegistry state.
	ServiceServiceCatalogAppRegistry = "servicecatalogappregistry"
)

const (
	// ResourceTypeServiceCatalogAppRegistryApplication identifies a Service
	// Catalog AppRegistry application metadata resource. The scanner emits
	// identity (id, ARN, name), the description, and lifecycle timestamps only.
	ResourceTypeServiceCatalogAppRegistryApplication = "aws_servicecatalog_appregistry_application"
	// ResourceTypeServiceCatalogAppRegistryAttributeGroup identifies a Service
	// Catalog AppRegistry attribute group metadata resource. The scanner emits
	// identity (id, ARN, name), the description, and lifecycle timestamps only.
	// Attribute-group content bodies (the application-metadata JSON document)
	// are never read or persisted.
	ResourceTypeServiceCatalogAppRegistryAttributeGroup = "aws_servicecatalog_appregistry_attribute_group"
)

const (
	// RelationshipServiceCatalogAppRegistryApplicationHasAttributeGroup records
	// an AppRegistry application's association with an attribute group. The
	// target is keyed by the attribute-group ARN so the edge joins the
	// attribute-group node the scanner publishes.
	RelationshipServiceCatalogAppRegistryApplicationHasAttributeGroup = "servicecatalog_appregistry_application_has_attribute_group"
	// RelationshipServiceCatalogAppRegistryApplicationAssociatesCloudFormationStack
	// records an AppRegistry application's association with a CloudFormation
	// stack. It is emitted only for CFN_STACK associated resources whose
	// reported ARN is a CloudFormation stack ARN, keyed by that stack ARN so the
	// edge joins the cloudformation scanner's published stack node.
	RelationshipServiceCatalogAppRegistryApplicationAssociatesCloudFormationStack = "servicecatalog_appregistry_application_associates_cloudformation_stack"
)
