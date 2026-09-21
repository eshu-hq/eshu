// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elasticbeanstalk

import (
	"context"
	"time"
)

// Client is the Elastic Beanstalk read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned types. The
// surface is metadata-only: it carries no mutation, environment-rebuild,
// CNAME-swap, or environment-info data-plane operation.
type Client interface {
	// DescribeApplications returns every Elastic Beanstalk application visible
	// to the configured credentials, including the configuration template
	// names declared on each application.
	DescribeApplications(context.Context) ([]Application, error)
	// DescribeEnvironments returns every environment visible to the configured
	// credentials.
	DescribeEnvironments(context.Context) ([]Environment, error)
	// DescribeApplicationVersions returns application version metadata visible
	// to the configured credentials.
	DescribeApplicationVersions(context.Context) ([]ApplicationVersion, error)
	// DescribeEnvironmentResources returns the concrete AWS resources reported
	// in use by one environment (Auto Scaling groups, launch templates, load
	// balancers). It is keyed by environment id.
	DescribeEnvironmentResources(ctx context.Context, environmentID string) (EnvironmentResources, error)
	// DescribeConfigurationSettings returns the deployed option settings for one
	// environment. The adapter strips option-setting values that are not needed
	// for relationship joins; the scanner redacts the remainder.
	DescribeConfigurationSettings(ctx context.Context, applicationName, environmentName string) ([]OptionSetting, error)
}

// Application is the scanner-owned representation of an Elastic Beanstalk
// application.
type Application struct {
	ARN                    string
	Name                   string
	Description            string
	ConfigurationTemplates []string
	VersionLabels          []string
	DateCreated            time.Time
	DateUpdated            time.Time
}

// Environment is the scanner-owned representation of an Elastic Beanstalk
// environment.
type Environment struct {
	ARN               string
	ID                string
	Name              string
	ApplicationName   string
	Status            string
	Health            string
	HealthStatus      string
	TierName          string
	TierType          string
	PlatformARN       string
	SolutionStackName string
	CNAME             string
	EndpointURL       string
	VersionLabel      string
	TemplateName      string
	OperationsRole    string
	DateCreated       time.Time
	DateUpdated       time.Time
}

// ApplicationVersion is the scanner-owned representation of an Elastic
// Beanstalk application version. Only metadata is carried; the source bundle
// object contents are never read.
type ApplicationVersion struct {
	ARN              string
	ApplicationName  string
	VersionLabel     string
	Description      string
	Status           string
	SourceS3Bucket   string
	SourceS3Key      string
	SourceRepository string
	BuildARN         string
	DateCreated      time.Time
	DateUpdated      time.Time
}

// EnvironmentResources carries the concrete AWS resources reported in use by
// one environment. Only resource identities used for relationship joins are
// kept.
type EnvironmentResources struct {
	AutoScalingGroupNames []string
	LaunchTemplateIDs     []string
	LoadBalancerNames     []string
}

// OptionSetting is the scanner-owned representation of one Elastic Beanstalk
// configuration option setting. The value is carried so the scanner can build
// relationship joins and redact secret-shaped option values; it must never be
// persisted in clear text.
type OptionSetting struct {
	Namespace  string
	OptionName string
	Value      string
}
