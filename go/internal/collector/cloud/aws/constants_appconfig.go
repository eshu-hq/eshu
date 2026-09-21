// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAppConfig identifies the regional AWS AppConfig metadata-only scan
	// slice. The scanner reads AppConfig control-plane identity and deployment
	// metadata through ListApplications, ListEnvironments,
	// ListConfigurationProfiles, ListDeploymentStrategies, and
	// ListTagsForResource. It never reads or persists configuration content,
	// hosted configuration version bodies, or freeform/feature-flag values, and
	// never mutates AppConfig state or starts deployments.
	ServiceAppConfig = "appconfig"
)

const (
	// ResourceTypeAppConfigApplication identifies an AWS AppConfig application
	// metadata resource. The scanner emits identity (id, synthesized ARN, name)
	// and description only; it never reads any configuration the application
	// distributes.
	ResourceTypeAppConfigApplication = "aws_appconfig_application"
	// ResourceTypeAppConfigEnvironment identifies an AWS AppConfig environment
	// metadata resource. The scanner emits identity, owning application id,
	// lifecycle state, and the CloudWatch alarm monitor references only.
	ResourceTypeAppConfigEnvironment = "aws_appconfig_environment"
	// ResourceTypeAppConfigConfigurationProfile identifies an AWS AppConfig
	// configuration profile metadata resource. The scanner emits identity,
	// owning application id, profile type, validator-type kinds, and the
	// configuration source location URI reference only; it never reads the
	// configuration content the profile points at.
	ResourceTypeAppConfigConfigurationProfile = "aws_appconfig_configuration_profile"
	// ResourceTypeAppConfigDeploymentStrategy identifies an AWS AppConfig
	// deployment strategy metadata resource. The scanner emits identity and the
	// rollout-shape knobs (duration, growth factor/type, final bake time,
	// replicate-to target) only.
	ResourceTypeAppConfigDeploymentStrategy = "aws_appconfig_deployment_strategy"
)

const (
	// RelationshipAppConfigEnvironmentInApplication records an AppConfig
	// environment's membership in its owning application. The target is keyed by
	// the application ARN the application node publishes so the edge joins the
	// application node.
	RelationshipAppConfigEnvironmentInApplication = "appconfig_environment_in_application"
	// RelationshipAppConfigProfileInApplication records an AppConfig
	// configuration profile's membership in its owning application. The target is
	// keyed by the application ARN the application node publishes.
	RelationshipAppConfigProfileInApplication = "appconfig_profile_in_application"
	// RelationshipAppConfigEnvironmentMonitorsAlarm records an AppConfig
	// environment's CloudWatch alarm monitor. AppConfig reports the alarm ARN,
	// which matches how the CloudWatch scanner publishes its alarm resource_id,
	// so the edge joins the alarm node.
	RelationshipAppConfigEnvironmentMonitorsAlarm = "appconfig_environment_monitors_alarm"
	// RelationshipAppConfigEnvironmentUsesMonitorRole records the IAM role
	// AppConfig assumes to read a monitored CloudWatch alarm. The target is keyed
	// by the role ARN the IAM scanner publishes.
	RelationshipAppConfigEnvironmentUsesMonitorRole = "appconfig_environment_uses_monitor_role"
)
