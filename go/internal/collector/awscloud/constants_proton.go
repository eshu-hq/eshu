// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
