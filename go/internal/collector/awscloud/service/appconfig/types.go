// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appconfig

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS AppConfig observations for one AWS claim.
// Implementations read control-plane identity and deployment metadata through
// the AppConfig list APIs and never read configuration content, hosted
// configuration version bodies, or freeform/feature-flag values.
type Client interface {
	// Snapshot returns every AppConfig application visible to the configured AWS
	// credentials, each carrying its environments and configuration profiles,
	// plus the account-level deployment strategies.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures AppConfig application metadata, account-level deployment
// strategies, and non-fatal scan warnings.
type Snapshot struct {
	// Applications is the metadata-only set of AppConfig applications, each
	// carrying its environments and configuration profiles.
	Applications []Application
	// DeploymentStrategies is the account-level, application-independent set of
	// AppConfig deployment strategies.
	DeploymentStrategies []DeploymentStrategy
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Application is the scanner-owned AppConfig application model. It carries
// control-plane identity metadata only and never any distributed configuration.
type Application struct {
	// ID is the AppConfig application id.
	ID string
	// Name is the AppConfig application name.
	Name string
	// Description is the optional application description.
	Description string
	// Environments are the metadata-only environments owned by this application.
	Environments []Environment
	// Profiles are the metadata-only configuration profiles owned by this
	// application.
	Profiles []ConfigurationProfile
	// Tags carries the application resource tags.
	Tags map[string]string
}

// Environment is the scanner-owned AppConfig environment model. It carries
// identity, lifecycle state, and the CloudWatch alarm monitor references only.
type Environment struct {
	// ID is the AppConfig environment id.
	ID string
	// ApplicationID is the owning AppConfig application id.
	ApplicationID string
	// Name is the AppConfig environment name.
	Name string
	// Description is the optional environment description.
	Description string
	// State is the current environment lifecycle state (for example
	// READY_FOR_DEPLOYMENT or DEPLOYING).
	State string
	// Monitors are the CloudWatch alarm monitors AppConfig watches during a
	// deployment to this environment.
	Monitors []Monitor
	// Tags carries the environment resource tags.
	Tags map[string]string
}

// Monitor is the scanner-owned AppConfig deployment monitor model. It records
// the CloudWatch alarm ARN AppConfig watches and the optional IAM role ARN
// AppConfig assumes to read that alarm. It carries no alarm threshold state or
// metric data.
type Monitor struct {
	// AlarmARN is the Amazon Resource Name of the watched CloudWatch alarm.
	AlarmARN string
	// AlarmRoleARN is the optional IAM role ARN AppConfig assumes to read the
	// alarm.
	AlarmRoleARN string
}

// ConfigurationProfile is the scanner-owned AppConfig configuration profile
// model. It carries identity, profile type, validator-type kinds, and the
// configuration source location URI reference only. The configuration content
// the profile points at is intentionally excluded.
type ConfigurationProfile struct {
	// ID is the AppConfig configuration profile id.
	ID string
	// ApplicationID is the owning AppConfig application id.
	ApplicationID string
	// Name is the configuration profile name.
	Name string
	// Type is the profile type (for example AWS.AppConfig.FeatureFlags or
	// AWS.Freeform).
	Type string
	// LocationURI is the configuration source location reference (for example an
	// SSM document, SSM parameter, S3 object URI, or "hosted"). It is a location
	// reference, never the configuration content itself.
	LocationURI string
	// ValidatorTypes are the validator-type kinds (for example JSON_SCHEMA or
	// LAMBDA) configured on the profile. Validator content/ARNs are excluded.
	ValidatorTypes []string
	// Tags carries the configuration profile resource tags.
	Tags map[string]string
}

// DeploymentStrategy is the scanner-owned AppConfig deployment strategy model.
// It carries identity and the rollout-shape knobs only.
type DeploymentStrategy struct {
	// ID is the AppConfig deployment strategy id.
	ID string
	// Name is the deployment strategy name.
	Name string
	// Description is the optional deployment strategy description.
	Description string
	// DeploymentDurationInMinutes is the total rollout duration.
	DeploymentDurationInMinutes int32
	// FinalBakeTimeInMinutes is the post-rollout alarm-monitoring bake window.
	FinalBakeTimeInMinutes int32
	// GrowthFactor is the percentage of targets that receive the configuration
	// during each rollout interval.
	GrowthFactor float32
	// GrowthType is the rollout growth algorithm (for example LINEAR or
	// EXPONENTIAL).
	GrowthType string
	// ReplicateTo is the deployment strategy persistence target (for example
	// NONE or SSM_DOCUMENT).
	ReplicateTo string
	// Tags carries the deployment strategy resource tags.
	Tags map[string]string
}
