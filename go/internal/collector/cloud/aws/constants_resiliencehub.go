// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
