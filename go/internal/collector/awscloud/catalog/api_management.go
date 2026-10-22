// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

const (
	// ServiceSynthetics identifies the regional Amazon CloudWatch Synthetics
	// metadata-only scan slice. The scanner reads canary control-plane metadata
	// through DescribeCanaries and never reads or persists canary script source
	// code, run artifacts (logs, screenshots, HAR files), or run results, and
	// never creates, updates, starts, stops, or deletes a canary.
	ServiceSynthetics = "synthetics"
)

const (
	// ResourceTypeSyntheticsCanary identifies an Amazon CloudWatch Synthetics
	// canary metadata resource. The scanner emits identity, runtime version,
	// status state, schedule expression and duration, retention, run resource
	// limits, and timeline timestamps only. Canary script source code and run
	// artifacts stay outside the contract.
	ResourceTypeSyntheticsCanary = "aws_synthetics_canary"
)

const (
	// RelationshipSyntheticsCanaryUsesS3Bucket records a Synthetics canary's
	// reported Amazon S3 artifact bucket dependency, where run artifacts (logs,
	// screenshots, HAR files) are stored. Synthetics reports an artifact
	// location path, so the scanner extracts the bucket name and keys the target
	// by the partition-aware bucket ARN the S3 scanner publishes.
	RelationshipSyntheticsCanaryUsesS3Bucket = "synthetics_canary_uses_s3_bucket"
	// RelationshipSyntheticsCanaryUsesIAMRole records a Synthetics canary's
	// execution IAM role dependency. AWS reports a role ARN, which matches how
	// the IAM scanner publishes its role resource_id.
	RelationshipSyntheticsCanaryUsesIAMRole = "synthetics_canary_uses_iam_role"
	// RelationshipSyntheticsCanaryUsesSubnet records a Synthetics canary's VPC
	// subnet dependency when the canary runs inside a VPC. The target is keyed by
	// the bare subnet id (subnet-...) the EC2 scanner publishes.
	RelationshipSyntheticsCanaryUsesSubnet = "synthetics_canary_uses_subnet"
	// RelationshipSyntheticsCanaryUsesSecurityGroup records a Synthetics canary's
	// VPC security group dependency when the canary runs inside a VPC. The target
	// is keyed by the bare security group id (sg-...) the EC2 scanner publishes.
	RelationshipSyntheticsCanaryUsesSecurityGroup = "synthetics_canary_uses_security_group"
)

const (
	// ServiceServiceCatalog identifies the regional AWS Service Catalog
	// metadata-only scan slice. The scanner emits portfolio, product, and
	// provisioned-product configuration metadata only; provisioning-artifact
	// template bodies, launch-constraint policy documents, provisioning
	// parameter values, and any record output values are never read or
	// persisted through this service kind.
	ServiceServiceCatalog = "servicecatalog"
)

const (
	// ResourceTypeServiceCatalogPortfolio identifies an AWS Service Catalog
	// portfolio metadata resource. The scanner emits portfolio identity, ARN,
	// display name, provider name, description, and creation timestamp only.
	ResourceTypeServiceCatalogPortfolio = "aws_servicecatalog_portfolio"
	// ResourceTypeServiceCatalogProduct identifies an AWS Service Catalog
	// product metadata resource. The scanner emits product identity, ARN, name,
	// product type, owner, distributor, status, and creation timestamp only;
	// provisioning-artifact template bodies stay outside the contract.
	ResourceTypeServiceCatalogProduct = "aws_servicecatalog_product"
	// ResourceTypeServiceCatalogProvisionedProduct identifies an AWS Service
	// Catalog provisioned-product metadata resource. The scanner emits
	// provisioned-product identity, ARN, status, type, product identifier,
	// provisioning-artifact identifier, provisioning-artifact name, deployed
	// CloudFormation stack physical identifier, and creation timestamp only;
	// provisioning parameter values and stack output values stay outside the
	// contract.
	ResourceTypeServiceCatalogProvisionedProduct = "aws_servicecatalog_provisioned_product"
)

const (
	// RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack
	// records that a provisioned product of type CFN_STACK deploys a
	// CloudFormation stack, derived from the provisioned product's
	// CloudFormation stack ARN physical identifier. The edge carries the stack
	// ARN identity only; no template body is read.
	RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack = "servicecatalog_provisioned_product_deploys_cloudformation_stack"
	// RelationshipServiceCatalogProductInPortfolio records a product's reported
	// association with a portfolio, derived from ListPortfoliosForProduct.
	RelationshipServiceCatalogProductInPortfolio = "servicecatalog_product_in_portfolio"
	// RelationshipServiceCatalogPortfolioGrantsPrincipal records a portfolio's
	// reported IAM role principal association, derived from
	// ListPrincipalsForPortfolio. The edge is emitted only when AWS reports a
	// fully defined IAM role ARN; IAM_PATTERN wildcard principals that name no
	// concrete role node stay outside the contract.
	RelationshipServiceCatalogPortfolioGrantsPrincipal = "servicecatalog_portfolio_grants_principal"
)

const (
	// ServiceProton identifies the AWS Proton control-plane metadata-only scan
	// slice. The scanner reads Proton management list/get APIs (ListEnvironments,
	// ListServices, GetService, ListEnvironmentTemplates, ListServiceTemplates,
	// ListServiceInstances) and never reads or persists service/environment spec
	// manifest bodies, template schema bodies, or deployment input parameter
	// values, and never mutates Proton state.
	ServiceProton = "proton"
)

const (
	// ResourceTypeProtonEnvironment identifies an AWS Proton environment metadata
	// resource. The scanner emits identity, the environment template name,
	// provisioning mode, deployment status, and the reported Proton service-role
	// ARN only; the environment spec manifest body is never read.
	ResourceTypeProtonEnvironment = "aws_proton_environment"
	// ResourceTypeProtonService identifies an AWS Proton service metadata
	// resource. The scanner emits identity, the service template name, status,
	// and (when GetService is reached) the source repository linkage by reference
	// only; the service spec manifest body and pipeline spec body are never read.
	ResourceTypeProtonService = "aws_proton_service"
	// ResourceTypeProtonEnvironmentTemplate identifies an AWS Proton environment
	// template metadata resource. The scanner emits identity, display name,
	// provisioning mode, and the recommended version only; template version
	// schema bodies are never read.
	ResourceTypeProtonEnvironmentTemplate = "aws_proton_environment_template"
	// ResourceTypeProtonServiceTemplate identifies an AWS Proton service template
	// metadata resource. The scanner emits identity, display name, pipeline
	// provisioning mode, and the recommended version only; template version
	// schema bodies are never read.
	ResourceTypeProtonServiceTemplate = "aws_proton_service_template"
)

const (
	// RelationshipProtonServiceInEnvironment records that a Proton service is
	// deployed into an environment through one of its service instances. The
	// target is keyed by the environment ARN the environment node publishes, so
	// the edge joins the environment node exactly. Service instances are read as
	// metadata only (name, service name, environment name); no instance spec or
	// input parameter value is persisted.
	RelationshipProtonServiceInEnvironment = "proton_service_in_environment"
	// RelationshipProtonEnvironmentUsesRole records a Proton environment's
	// reported Proton service-role dependency. AWS reports an IAM role ARN, which
	// matches how the IAM scanner publishes its role resource_id, so the edge
	// joins the role node by ARN.
	RelationshipProtonEnvironmentUsesRole = "proton_environment_uses_role"
)

const (
	// ServiceResilienceHub identifies the regional AWS Resilience Hub
	// metadata-only scan slice. The scanner reads control-plane describe/list
	// APIs for applications, resiliency policies, application components, input
	// sources, and assessments, plus the published-version physical-resource
	// inventory, and never persists assessment result bodies, drift detail,
	// recommendation contents, or any data-plane payload.
	ServiceResilienceHub = "resiliencehub"

	// WarningResilienceHubAppVersionMissing marks a Resilience Hub application
	// that has no published version, so its version-scoped input sources,
	// components, and protected physical resources were omitted for the scan.
	// The application's own summary metadata is still emitted.
	WarningResilienceHubAppVersionMissing = "resiliencehub_app_version_missing"
)

const (
	// ResourceTypeResilienceHubApp identifies an AWS Resilience Hub application
	// metadata resource. The scanner emits identity, status, drift/compliance
	// status labels, the configured RPO/RTO targets, the assessment schedule,
	// and the resiliency-score number only.
	ResourceTypeResilienceHubApp = "aws_resiliencehub_app"
	// ResourceTypeResilienceHubResiliencyPolicy identifies an AWS Resilience Hub
	// resiliency policy metadata resource. The scanner emits identity, the
	// policy tier, estimated cost tier, data-location constraint, and the set of
	// failure-policy keys (AZ/Hardware/Software/Region) with their RPO/RTO
	// targets only.
	ResourceTypeResilienceHubResiliencyPolicy = "aws_resiliencehub_resiliency_policy"
	// ResourceTypeResilienceHubAppComponent identifies an AWS Resilience Hub
	// application component metadata resource. The scanner emits the component
	// name and type only.
	ResourceTypeResilienceHubAppComponent = "aws_resiliencehub_app_component"
	// ResourceTypeResilienceHubAppInputSource identifies an AWS Resilience Hub
	// application input source metadata resource (a CloudFormation stack,
	// Resource Group, AppRegistry application, Terraform state file, or EKS
	// cluster the application draws its resources from). The scanner emits the
	// import type, source name, source ARN, and reported resource count only.
	ResourceTypeResilienceHubAppInputSource = "aws_resiliencehub_app_input_source"
	// ResourceTypeResilienceHubAppAssessment identifies an AWS Resilience Hub
	// application assessment metadata resource. The scanner emits identity,
	// assessment status, compliance status, drift status, invoker, and the
	// resiliency score only; assessment result bodies and drift detail stay
	// outside the contract.
	ResourceTypeResilienceHubAppAssessment = "aws_resiliencehub_app_assessment"
)

const (
	// RelationshipResilienceHubAppUsesPolicy records that a Resilience Hub
	// application is governed by a resiliency policy. The target is keyed by the
	// policy ARN, matching how the resiliency-policy node publishes its
	// resource_id, so the edge joins inside this scanner's own resources.
	RelationshipResilienceHubAppUsesPolicy = "resiliencehub_app_uses_policy"
	// RelationshipResilienceHubAppProtectsResource records that a Resilience Hub
	// application protects a physical AWS resource (for example an ECS service,
	// EFS file system, ELBv2 load balancer, Lambda function, or SNS topic). The
	// target is keyed by the resource ARN Resilience Hub reports, matching how
	// the owning resource scanner publishes its resource_id. Resilience
	// Hub-native (non-ARN) physical identifiers are recorded only as app
	// attributes and never keyed as edges, so the graph never dangles.
	RelationshipResilienceHubAppProtectsResource = "resiliencehub_app_protects_resource"
	// RelationshipResilienceHubComponentInApp records an application component's
	// membership in its parent Resilience Hub application. The target is keyed by
	// the application ARN the application node publishes.
	RelationshipResilienceHubComponentInApp = "resiliencehub_component_in_app"
	// RelationshipResilienceHubInputSourceInApp records an input source's
	// membership in its parent Resilience Hub application. The target is keyed by
	// the application ARN the application node publishes.
	RelationshipResilienceHubInputSourceInApp = "resiliencehub_input_source_in_app"
	// RelationshipResilienceHubAssessmentForApp records that an assessment was
	// run for a Resilience Hub application. The target is keyed by the
	// application ARN the application node publishes.
	RelationshipResilienceHubAssessmentForApp = "resiliencehub_assessment_for_app"
)

const (
	// ServiceFIS identifies the regional AWS Fault Injection Service (FIS)
	// metadata-only scan slice. The scanner reads experiment-template
	// control-plane metadata through the FIS management APIs
	// (ListExperimentTemplates, GetExperimentTemplate, ListTagsForResource) and
	// never starts, stops, or mutates an experiment, and never reads experiment
	// run results, resolved-target inventories, or any data-plane payload.
	ServiceFIS = "fis"
)

const (
	// ResourceTypeFISExperimentTemplate identifies an AWS FIS experiment
	// template metadata resource. The scanner emits identity (id, ARN, name from
	// the Name tag, description), the execution IAM role reference, the action
	// and target id/type inventory, the logging and stop-condition destinations,
	// and lifecycle timestamps only. Action parameter values, target filter
	// values, and experiment run outputs stay outside the contract.
	ResourceTypeFISExperimentTemplate = "aws_fis_experiment_template"
)

const (
	// RelationshipFISTemplateUsesIAMRole records the IAM role an FIS experiment
	// template assumes to inject faults. The target is keyed by the role ARN FIS
	// reports, matching the IAM scanner's published role resource_id.
	RelationshipFISTemplateUsesIAMRole = "fis_template_uses_iam_role"
	// RelationshipFISTemplateTargetsEC2Instance records an FIS experiment
	// template's explicit EC2 instance target. The target is keyed by the bare
	// instance id (i-...) extracted from the reported instance ARN, matching the
	// identity EC2 instance edges publish across the fleet.
	RelationshipFISTemplateTargetsEC2Instance = "fis_template_targets_ec2_instance"
	// RelationshipFISTemplateTargetsECSCluster records an FIS experiment
	// template's explicit ECS cluster target. The target is keyed by the cluster
	// ARN FIS reports, matching the ECS scanner's published cluster resource_id.
	RelationshipFISTemplateTargetsECSCluster = "fis_template_targets_ecs_cluster"
	// RelationshipFISTemplateTargetsRDSDBInstance records an FIS experiment
	// template's explicit RDS DB instance target. The target is keyed by the DB
	// instance ARN FIS reports, matching the RDS scanner's published instance
	// resource_id.
	RelationshipFISTemplateTargetsRDSDBInstance = "fis_template_targets_rds_db_instance"
	// RelationshipFISTemplateTargetsRDSDBCluster records an FIS experiment
	// template's explicit RDS DB cluster target. The target is keyed by the DB
	// cluster ARN FIS reports, matching the RDS scanner's published cluster
	// resource_id.
	RelationshipFISTemplateTargetsRDSDBCluster = "fis_template_targets_rds_db_cluster"
	// RelationshipFISTemplateLogsToCloudWatchLogGroup records the CloudWatch
	// Logs log group an FIS experiment template streams experiment logs to. The
	// target is keyed by the log group ARN FIS reports, matching the
	// cloudwatchlogs scanner's published log group resource_id.
	RelationshipFISTemplateLogsToCloudWatchLogGroup = "fis_template_logs_to_cloudwatch_log_group"
	// RelationshipFISTemplateLogsToS3 records the S3 bucket an FIS experiment
	// template writes experiment logs to. FIS reports a bucket name, so the
	// scanner synthesizes the partition-aware bucket ARN to match the S3
	// scanner's published bucket resource_id.
	RelationshipFISTemplateLogsToS3 = "fis_template_logs_to_s3"
	// RelationshipFISTemplateStopsOnCloudWatchAlarm records a CloudWatch alarm
	// an FIS experiment template halts on as a stop condition. The target is
	// keyed by the alarm ARN FIS reports, matching the cloudwatch scanner's
	// published alarm resource_id.
	RelationshipFISTemplateStopsOnCloudWatchAlarm = "fis_template_stops_on_cloudwatch_alarm"
)
