// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

const (
	// ServiceCloudFormation identifies the regional AWS CloudFormation metadata
	// scan slice. CloudFormation is the highest template-body redaction surface
	// in the AWS collector: stack and stack-set templates can carry inline IAM
	// policy bodies, NoEcho parameter values, and embedded secrets. The scanner
	// emits stack, stack-set, change-set, drift-result, stack-instance, and
	// registered-type configuration metadata only. Template bodies
	// (GetTemplate / TemplateBody), parameter values, change-set bodies, and
	// drift property documents are the protected data class and are never read
	// or persisted through this service kind.
	ServiceCloudFormation = "cloudformation"
)

const (
	// ResourceTypeCloudFormationStack identifies a CloudFormation stack metadata
	// resource. The scanner emits stack identity, status, capabilities, tags,
	// role ARN, drift status, and timestamps only; the template body, parameter
	// values, and secret-like output values are never persisted.
	ResourceTypeCloudFormationStack = "aws_cloudformation_stack"
	// ResourceTypeCloudFormationStackSet identifies a CloudFormation stack set
	// metadata resource. The scanner emits stack-set identity, status,
	// capabilities, permission model, and administration/execution role
	// references only; the stack-set template body and parameter values are
	// never persisted.
	ResourceTypeCloudFormationStackSet = "aws_cloudformation_stack_set"
	// ResourceTypeCloudFormationChangeSet identifies a CloudFormation change set
	// metadata resource. The scanner emits change-set identity, status, and
	// execution status only; the per-resource change body (action, replacement
	// detail) is never read or persisted.
	ResourceTypeCloudFormationChangeSet = "aws_cloudformation_change_set"
	// ResourceTypeCloudFormationStackDrift identifies a CloudFormation stack
	// drift detection result metadata resource. The scanner emits drift status
	// and per-status resource counts only; actual and expected property
	// documents and per-property differences are never persisted.
	ResourceTypeCloudFormationStackDrift = "aws_cloudformation_stack_drift"
	// ResourceTypeCloudFormationStackInstance identifies a CloudFormation stack
	// set instance metadata resource. The scanner emits account, region, status,
	// and drift status only.
	ResourceTypeCloudFormationStackInstance = "aws_cloudformation_stack_instance"
	// ResourceTypeCloudFormationType identifies a CloudFormation registered
	// extension (type) metadata resource. The scanner emits type identity,
	// kind, default version, publisher, and activation state only; type schema
	// and configuration bodies are never persisted.
	ResourceTypeCloudFormationType = "aws_cloudformation_type"
)

const (
	// RelationshipCloudFormationStackUsesResourceType records that a stack
	// manages at least one resource of a given CloudFormation resource type,
	// derived from ListStackResources. The relationship carries the resource
	// type and physical/logical identity only; no template body is read.
	RelationshipCloudFormationStackUsesResourceType = "cloudformation_stack_uses_resource_type"
	// RelationshipCloudFormationStackSetContainsStackInstance records that a
	// stack set owns a deployed stack instance in a target account and region.
	RelationshipCloudFormationStackSetContainsStackInstance = "cloudformation_stack_set_contains_stack_instance"
	// RelationshipCloudFormationStackUsesIAMRole records a stack's reported IAM
	// service role ARN dependency.
	RelationshipCloudFormationStackUsesIAMRole = "cloudformation_stack_uses_iam_role"
	// RelationshipCloudFormationStackUsesS3TemplateURL records a stack's reported
	// S3 template URL reference. The URL location is provenance evidence; the
	// referenced template object contents are never fetched.
	RelationshipCloudFormationStackUsesS3TemplateURL = "cloudformation_stack_uses_s3_template_url"
)

const (
	// ServiceConfig identifies the regional AWS Config metadata scan slice.
	ServiceConfig = "config"
)

const (
	// ResourceTypeConfigConfigurationRecorder identifies an AWS Config
	// configuration recorder. The recorder names the resource types Config
	// records; it never carries recorded configuration item bodies.
	ResourceTypeConfigConfigurationRecorder = "aws_config_configuration_recorder"
	// ResourceTypeConfigDeliveryChannel identifies an AWS Config delivery
	// channel: the S3 bucket and optional SNS topic Config delivers snapshots
	// and history files to.
	ResourceTypeConfigDeliveryChannel = "aws_config_delivery_channel"
	// ResourceTypeConfigRule identifies an AWS Config rule. The rule carries its
	// owner (AWS managed, custom Lambda, or custom policy), the managed-rule
	// source identifier or custom-Lambda function ARN, and the resource-type
	// scope it evaluates. Compliance evaluation result bodies are never stored.
	ResourceTypeConfigRule = "aws_config_rule"
	// ResourceTypeConfigConformancePack identifies an AWS Config conformance
	// pack: a named collection of Config rules with a deployment status and a
	// member-rule count. Conformance pack template bodies are never stored.
	ResourceTypeConfigConformancePack = "aws_config_conformance_pack"
	// ResourceTypeConfigConfigurationAggregator identifies an AWS Config
	// configuration aggregator that aggregates Config data from source accounts
	// and regions or from an organization.
	ResourceTypeConfigConfigurationAggregator = "aws_config_configuration_aggregator"
	// ResourceTypeConfigRetentionConfiguration identifies an AWS Config
	// retention configuration: the number of days Config retains configuration
	// item history.
	ResourceTypeConfigRetentionConfiguration = "aws_config_retention_configuration"
)

const (
	// RelationshipConfigConformancePackContainsRule records that an AWS Config
	// conformance pack contains a member Config rule. The target is the
	// aws_config_rule resource keyed by rule name.
	RelationshipConfigConformancePackContainsRule = "config_conformance_pack_contains_rule"
	// RelationshipConfigRuleEvaluatedByLambda records that an AWS Config custom
	// Lambda rule is evaluated by a Lambda function. The target is the
	// aws_lambda_function resource keyed by the Lambda function ARN.
	RelationshipConfigRuleEvaluatedByLambda = "config_rule_evaluated_by_lambda"
	// RelationshipConfigAggregatorSourcesAccount records that an AWS Config
	// configuration aggregator aggregates data from a source AWS account. The
	// target is the aws_account resource keyed by the account root ARN.
	RelationshipConfigAggregatorSourcesAccount = "config_aggregator_sources_account"
)

const (
	// ServiceControlTower identifies the regional AWS Control Tower metadata-only
	// scan slice. The scanner reads landing-zone, enabled-control, and
	// enabled-baseline control-plane metadata through the controltower management
	// APIs (ListLandingZones, GetLandingZone, ListEnabledBaselines,
	// ListEnabledControls, ListTagsForResource) and never reads or persists the
	// landing-zone manifest body, control or baseline parameter values, or any
	// account governance payload, and never enables, disables, resets, or
	// mutates Control Tower state.
	ServiceControlTower = "controltower"
)

const (
	// ResourceTypeControlTowerLandingZone identifies an AWS Control Tower landing
	// zone metadata resource. The scanner emits identity (ARN), deployed version,
	// lifecycle status, and drift status only. The landing-zone manifest JSON
	// body, which carries governance configuration, stays outside the contract.
	ResourceTypeControlTowerLandingZone = "aws_controltower_landing_zone"
	// ResourceTypeControlTowerEnabledControl identifies an AWS Control Tower
	// enabled control metadata resource. The scanner emits identity (the enabled
	// control ARN), the control identifier, the governed organizational-unit
	// target, and deployment and drift status only. Control parameter values stay
	// outside the contract.
	ResourceTypeControlTowerEnabledControl = "aws_controltower_enabled_control"
	// ResourceTypeControlTowerEnabledBaseline identifies an AWS Control Tower
	// enabled baseline metadata resource. The scanner emits identity (the enabled
	// baseline ARN), the baseline identifier and enabled version, the governed
	// target, and deployment and drift status only. Baseline parameter values
	// stay outside the contract.
	ResourceTypeControlTowerEnabledBaseline = "aws_controltower_enabled_baseline"
)

const (
	// RelationshipControlTowerControlGovernsTarget records that an AWS Control
	// Tower enabled control governs an Organizations organizational unit (or, for
	// account-scoped targets, an account). The target is keyed by the bare
	// Organizations id (ou-…, account id, or r-…) parsed from the Control Tower
	// target ARN so the edge joins the node the organizations scanner publishes.
	RelationshipControlTowerControlGovernsTarget = "controltower_control_governs_target"
	// RelationshipControlTowerBaselineGovernsTarget records that an AWS Control
	// Tower enabled baseline governs an Organizations organizational unit, account,
	// or root. The target is keyed by the bare Organizations id parsed from the
	// Control Tower target ARN so the edge joins the organizations scanner node.
	RelationshipControlTowerBaselineGovernsTarget = "controltower_baseline_governs_target"
	// RelationshipControlTowerBaselineForLandingZone records that an AWS Control
	// Tower enabled baseline belongs to a landing zone in the same boundary. The
	// edge is internal to this scanner and is keyed by the landing-zone ARN the
	// landing-zone node publishes.
	RelationshipControlTowerBaselineForLandingZone = "controltower_baseline_for_landing_zone"
)

const (
	// ServiceOrganizations identifies the us-east-1 AWS Organizations metadata
	// scan slice for management or delegated-administrator accounts.
	ServiceOrganizations = "organizations"
)

const (
	// ResourceTypeOrganizationsRoot identifies an AWS Organizations root.
	ResourceTypeOrganizationsRoot = "aws_organizations_root"
	// ResourceTypeOrganizationsOrganizationalUnit identifies an AWS
	// Organizations organizational unit.
	ResourceTypeOrganizationsOrganizationalUnit = "aws_organizations_organizational_unit"
	// ResourceTypeOrganizationsAccount identifies an AWS Organizations member
	// account.
	ResourceTypeOrganizationsAccount = "aws_organizations_account"
	// ResourceTypeOrganizationsPolicy identifies an AWS Organizations policy
	// summary. Policy document bodies are not part of this resource.
	ResourceTypeOrganizationsPolicy = "aws_organizations_policy"
	// ResourceTypeOrganizationsDelegatedAdministrator identifies an AWS
	// Organizations delegated administrator binding.
	ResourceTypeOrganizationsDelegatedAdministrator = "aws_organizations_delegated_administrator"
)

const (
	// RelationshipOrganizationsAccountInOU records Organizations account
	// membership in an organizational unit.
	RelationshipOrganizationsAccountInOU = "organizations_account_in_ou"
	// RelationshipOrganizationsOUInOU records Organizations OU child
	// membership in a parent OU.
	RelationshipOrganizationsOUInOU = "organizations_ou_in_ou"
	// RelationshipOrganizationsAccountInRoot records Organizations account
	// membership directly under a root.
	RelationshipOrganizationsAccountInRoot = "organizations_account_in_root"
	// RelationshipOrganizationsPolicyTargetsResource records Organizations
	// policy attachment metadata to a root, OU, or account.
	RelationshipOrganizationsPolicyTargetsResource = "organizations_policy_targets_resource"
	// RelationshipOrganizationsDelegatedAdminForAccount records a delegated
	// administrator binding to its member account.
	RelationshipOrganizationsDelegatedAdminForAccount = "organizations_delegated_admin_for_account"
)

const (
	// ServiceRAM identifies the regional AWS Resource Access Manager metadata
	// scan slice for one claimed account and region.
	ServiceRAM = "ram"
)

const (
	// ResourceTypeRAMResourceShare identifies an AWS Resource Access Manager
	// resource share metadata resource. Permission policy document bodies are
	// never part of this resource.
	ResourceTypeRAMResourceShare = "aws_ram_resource_share"
	// ResourceTypeRAMPermission identifies an AWS Resource Access Manager
	// managed-permission metadata summary (name, ARN, version, type). The
	// permission policy document body is never part of this resource.
	ResourceTypeRAMPermission = "aws_ram_permission"
)

const (
	// RelationshipRAMShareIncludesResource records that a resource share shares
	// one resource, targeting the shared resource's ARN with its RAM-reported
	// resource type as the relationship target type.
	RelationshipRAMShareIncludesResource = "ram_share_includes_resource"
	// RelationshipRAMShareTargetsAccount records that a resource share targets
	// one principal AWS account, targeting the bare account id that the
	// organizations scanner emits as an account resource id.
	RelationshipRAMShareTargetsAccount = "ram_share_targets_account"
	// RelationshipRAMShareTargetsOrganizationalUnit records that a resource
	// share targets one Organizations organizational unit principal, targeting
	// the OU ARN.
	RelationshipRAMShareTargetsOrganizationalUnit = "ram_share_targets_organizational_unit"
	// RelationshipRAMShareTargetsOrganization records that a resource share
	// targets an entire Organizations organization or root principal, targeting
	// the organization or root ARN.
	RelationshipRAMShareTargetsOrganization = "ram_share_targets_organization"
	// RelationshipRAMShareTargetsPrincipal records that a resource share targets
	// a principal whose form RAM did not report as a bare account id, an
	// Organizations OU ARN, or an organization or root ARN (for example a
	// service principal or a future RAM principal form). It uses the generic
	// resource target type and the raw principal id as the join key so the edge
	// stays honest without masquerading as an Organizations account.
	RelationshipRAMShareTargetsPrincipal = "ram_share_targets_principal"
	// RelationshipRAMShareUsesPermission records that a resource share uses one
	// managed permission, targeting the permission ARN.
	RelationshipRAMShareUsesPermission = "ram_share_uses_permission"
)

const (
	// ServiceServiceQuotas identifies the regional AWS Service Quotas
	// metadata-only scan slice. The scanner reads the Service Quotas
	// control-plane (ListServices, ListServiceQuotas, ListAWSDefaultServiceQuotas)
	// to report the applied quota values for each AWS service in the claimed
	// account and region, joined against the AWS-published defaults so an operator
	// can see which quotas were raised. It never requests, modifies, or deletes a
	// quota, and never associates a quota-increase template.
	ServiceServiceQuotas = "servicequotas"
)

const (
	// ResourceTypeServiceQuotasServiceQuota identifies one applied AWS Service
	// Quotas quota for a service in a claimed account and region. The resource
	// carries the quota identity (service code, quota code, quota name, quota
	// ARN), the applied value, the AWS-published default value, an override flag
	// that is true when the applied value differs from the default, the
	// adjustable/global flags, the unit, the optional rate period, the applied
	// level (ACCOUNT/RESOURCE/ALL), the optional resource-level quota context, and
	// the CloudWatch usage-metric identity (namespace, name, dimensions,
	// statistic) AWS recommends for tracking usage. Quota values and metric
	// identities are limit metadata, not workload data.
	ResourceTypeServiceQuotasServiceQuota = "aws_servicequotas_service_quota"
)

const (
	// ServiceResourceGroups identifies the regional AWS Resource Groups
	// metadata-only scan slice covering resource groups, their query type
	// (tag-filter or CloudFormation-stack backed), and the membership edges that
	// connect each group to its member resources. Group query bodies (the tag
	// filter or CloudFormation template JSON) are never persisted; only the query
	// type and the member resource ARNs the API reports are recorded.
	ServiceResourceGroups = "resourcegroups"
)

const (
	// ResourceTypeResourceGroupsGroup identifies an AWS Resource Groups group
	// metadata resource. The scanner emits identity (name, ARN, description) and
	// the group's query type only; the resource-query body is never persisted.
	ResourceTypeResourceGroupsGroup = "aws_resourcegroups_group"
)

const (
	// RelationshipResourceGroupsGroupContainsResource records that a Resource
	// Groups group counts a member resource as part of the group, as reported by
	// the ListGroupResources API. The target is typed from the member resource's
	// ARN family and keyed by the identity that family's scanner publishes
	// (ARN-equality for ARN-keyed families, bare or prefixed id otherwise). The
	// edge is only emitted when the member family resolves to a declared resource
	// type; members of an unrecognized family are skipped rather than emitted with
	// an empty target type.
	RelationshipResourceGroupsGroupContainsResource = "resourcegroups_group_contains_resource"
	// RelationshipResourceGroupsGroupBackedByStack records that a Resource Groups
	// group is backed by a CloudFormation stack (the group's query type is
	// CLOUDFORMATION_STACK_1_0). The target is the CloudFormation stack the group
	// tracks, keyed by the stack ARN the cloudformation scanner publishes.
	RelationshipResourceGroupsGroupBackedByStack = "resourcegroups_group_backed_by_stack"
)

const (
	// ServiceLicenseManager identifies the regional AWS License Manager
	// metadata-only scan slice. The scanner reads license-configuration
	// control-plane metadata through the License Manager management APIs
	// (ListLicenseConfigurations, ListAssociationsForLicenseConfiguration,
	// ListTagsForResource) and never grants, checks out, mutates, or otherwise
	// changes license state, and never reads license entitlement tokens or
	// usage records.
	ServiceLicenseManager = "licensemanager"
)

const (
	// ResourceTypeLicenseManagerConfiguration identifies an AWS License Manager
	// license configuration metadata resource. The scanner emits identity, the
	// license counting dimension, the configured and consumed license counts,
	// the hard-limit and status flags, and the count of associated resources
	// only. License rules and product-information match expressions are recorded
	// as structural metadata; no entitlement token or usage record is read.
	ResourceTypeLicenseManagerConfiguration = "aws_license_manager_configuration"
)

const (
	// RelationshipLicenseManagerConfigurationAppliesToInstance records that a
	// License Manager license configuration is associated with an EC2 instance.
	// The edge targets the EC2 instance by its bare instance id (i-...), which is
	// how EC2 instance nodes are keyed, so the edge joins the instance node
	// instead of dangling.
	RelationshipLicenseManagerConfigurationAppliesToInstance = "license_manager_configuration_applies_to_instance"
)

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
