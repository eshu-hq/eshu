// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package proton

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Proton control-plane observations for one
// AWS claim. Implementations read Proton management list/get APIs only and never
// read or persist service/environment spec manifest bodies, template schema
// bodies, or deployment input parameter values.
type Client interface {
	// Snapshot returns every Proton environment, service, environment template,
	// and service template visible to the configured AWS credentials, plus the
	// service-to-environment placements derived from service instances.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures AWS Proton control-plane metadata plus non-fatal scan
// warnings. It carries no spec manifest, template schema, or input parameter
// values.
type Snapshot struct {
	// Environments is the metadata-only set of Proton environments.
	Environments []Environment
	// Services is the metadata-only set of Proton services.
	Services []Service
	// EnvironmentTemplates is the metadata-only set of Proton environment
	// templates.
	EnvironmentTemplates []Template
	// ServiceTemplates is the metadata-only set of Proton service templates.
	ServiceTemplates []Template
	// ServicePlacements records the service-to-environment deployments derived
	// from Proton service instances. Each entry carries only the service name and
	// the environment name, never the instance spec or input parameter values.
	ServicePlacements []ServicePlacement
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Environment is the scanner-owned Proton environment model. It carries
// control-plane metadata only and intentionally excludes the environment spec
// manifest body and any deployment input parameter values.
type Environment struct {
	// ARN is the Amazon Resource Name that uniquely identifies the environment.
	ARN string
	// Name is the Proton environment name.
	Name string
	// TemplateName is the name of the environment template the environment was
	// created from.
	TemplateName string
	// TemplateMajorVersion is the major version of the environment template.
	TemplateMajorVersion string
	// TemplateMinorVersion is the minor version of the environment template.
	TemplateMinorVersion string
	// Provisioning reports the environment provisioning mode (for example
	// CUSTOMER_MANAGED) when AWS reports one.
	Provisioning string
	// DeploymentStatus is the environment deployment lifecycle status (for
	// example SUCCEEDED). The free-form deployment status message is excluded.
	DeploymentStatus string
	// Description is the optional environment description.
	Description string
	// ProtonServiceRoleArn is the IAM role ARN Proton assumes to make calls on
	// the account's behalf for this environment, when reported.
	ProtonServiceRoleArn string
	// EnvironmentAccountID is the account id the environment infrastructure is
	// provisioned in, when reported (cross-account environments).
	EnvironmentAccountID string
	// CreatedAt is when the environment was created.
	CreatedAt time.Time
	// LastDeploymentSucceededAt is when the environment last deployed
	// successfully, when reported.
	LastDeploymentSucceededAt time.Time
	// Tags carries the environment resource tags.
	Tags map[string]string
}

// Service is the scanner-owned Proton service model. It carries control-plane
// metadata only and intentionally excludes the service spec manifest body, the
// pipeline spec body, and any deployment input parameter values.
type Service struct {
	// ARN is the Amazon Resource Name that uniquely identifies the service.
	ARN string
	// Name is the Proton service name.
	Name string
	// TemplateName is the name of the service template the service was created
	// from.
	TemplateName string
	// Status is the service lifecycle status (for example ACTIVE). The free-form
	// status message is excluded.
	Status string
	// Description is the optional service description.
	Description string
	// BranchName is the source repository branch Proton syncs the service from,
	// when reported. It is a reference, not source content.
	BranchName string
	// RepositoryID is the source repository identifier (owner/name) Proton syncs
	// the service from, when reported. It is a reference, not source content.
	RepositoryID string
	// RepositoryConnectionArn is the CodeStar connection ARN Proton uses to reach
	// the source repository, when reported. It is a reference, not a credential.
	RepositoryConnectionArn string
	// CreatedAt is when the service was created.
	CreatedAt time.Time
	// LastModifiedAt is when the service was last modified.
	LastModifiedAt time.Time
	// Tags carries the service resource tags.
	Tags map[string]string
}

// Template is the scanner-owned Proton template model, shared by environment and
// service templates. It carries control-plane metadata only and intentionally
// excludes every template version schema body.
type Template struct {
	// ARN is the Amazon Resource Name that uniquely identifies the template.
	ARN string
	// Name is the Proton template name.
	Name string
	// DisplayName is the human-friendly template display name, when reported.
	DisplayName string
	// Description is the optional template description.
	Description string
	// Provisioning reports the template provisioning mode when AWS reports one.
	// For service templates this is the pipeline provisioning mode.
	Provisioning string
	// RecommendedVersion is the recommended template version, when reported.
	RecommendedVersion string
	// CreatedAt is when the template was created.
	CreatedAt time.Time
	// LastModifiedAt is when the template was last modified.
	LastModifiedAt time.Time
	// Tags carries the template resource tags.
	Tags map[string]string
}

// ServicePlacement records that a Proton service is deployed into an environment
// through one of its service instances. It carries only the joining names, never
// the instance spec or input parameter values.
type ServicePlacement struct {
	// ServiceName is the Proton service the instance belongs to.
	ServiceName string
	// EnvironmentName is the Proton environment the instance is deployed into.
	EnvironmentName string
}
