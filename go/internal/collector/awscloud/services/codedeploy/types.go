// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedeploy

import (
	"context"
	"time"
)

// Client is the CodeDeploy read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned metadata records. The
// contract lists no mutation, data-plane, or revision-body call so the scanner
// cannot reach beyond metadata.
type Client interface {
	// ListApplications returns CodeDeploy application metadata for the boundary.
	ListApplications(context.Context) ([]Application, error)
	// ListDeploymentGroups returns deployment-group metadata for one
	// application. The application name scopes the deployment groups AWS
	// reports.
	ListDeploymentGroups(context.Context, string) ([]DeploymentGroup, error)
	// ListDeploymentConfigs returns deployment-configuration metadata for the
	// boundary, including AWS-managed and customer-defined configs.
	ListDeploymentConfigs(context.Context) ([]DeploymentConfig, error)
	// ListRecentDeployments returns recent deployment metadata for the
	// boundary. Implementations bound the result to a recent window so the
	// scanner stays metadata-sized.
	ListRecentDeployments(context.Context) ([]Deployment, error)
}

// Application is the scanner-owned representation of one CodeDeploy
// application. It carries identity and compute-platform metadata only.
type Application struct {
	Name            string
	ID              string
	ComputePlatform string
	GitHubAccount   string
	LinkedToGitHub  bool
	CreateTime      time.Time
	Tags            map[string]string
}

// DeploymentGroup is the scanner-owned representation of one CodeDeploy
// deployment group. It carries deployment style, rollback configuration, and
// target evidence. On-premises instance tag values are redacted before they
// reach this record; see OnPremisesTagFilterSummary.
type DeploymentGroup struct {
	Name                       string
	ID                         string
	ApplicationName            string
	ComputePlatform            string
	DeploymentConfigName       string
	ServiceRoleARN             string
	DeploymentStyle            DeploymentStyle
	AutoRollback               AutoRollbackConfig
	OutdatedInstancesStrategy  string
	TerminationHookEnabled     bool
	AutoScalingGroups          []string
	ECSServices                []ECSServiceTarget
	LambdaFunctions            []string
	SNSTriggers                []SNSTrigger
	EC2TagFilterSummary        []TagFilterSummary
	OnPremisesTagFilterSummary []TagFilterSummary
	Tags                       map[string]string
}

// DeploymentStyle captures whether a deployment group runs in-place or
// blue/green and whether traffic routes behind a load balancer.
type DeploymentStyle struct {
	DeploymentType   string
	DeploymentOption string
}

// AutoRollbackConfig captures the automatic-rollback configuration reported for
// a deployment group.
type AutoRollbackConfig struct {
	Enabled bool
	Events  []string
}

// ECSServiceTarget names one Amazon ECS cluster/service pair targeted by a
// deployment group on the ECS compute platform.
type ECSServiceTarget struct {
	ClusterName string
	ServiceName string
}

// SNSTrigger names one SNS topic wired to a deployment-group trigger
// configuration. The scanner emits trigger names and topic ARNs only.
type SNSTrigger struct {
	Name     string
	TopicARN string
	Events   []string
}

// TagFilterSummary is a redaction-safe summary of one EC2 or on-premises tag
// filter. The Key and Type are AWS-controlled metadata. ValueMarker is the
// redaction marker for the filter value when present; the raw value never
// reaches this record so customer-PII tag values are not persisted.
type TagFilterSummary struct {
	Key         string
	Type        string
	HasValue    bool
	ValueMarker map[string]any
}

// DeploymentConfig is the scanner-owned representation of one CodeDeploy
// deployment configuration, carrying the minimum-healthy-hosts contract only.
type DeploymentConfig struct {
	Name                    string
	ID                      string
	ComputePlatform         string
	MinimumHealthyHostType  string
	MinimumHealthyHostValue int32
	CreateTime              time.Time
}

// Deployment is the scanner-owned representation of one recent CodeDeploy
// deployment. RevisionSummary carries safe revision-source references only;
// appspec.yml bodies and raw-string revision content are never present.
type Deployment struct {
	ID                   string
	ApplicationName      string
	DeploymentGroupName  string
	DeploymentConfigName string
	Status               string
	Creator              string
	ComputePlatform      string
	CreateTime           time.Time
	CompleteTime         time.Time
	RevisionSummary      RevisionSummary
}

// RevisionSummary captures non-secret revision-source references. The
// CodeDeploy adapter must never copy appspec.yml or raw-string revision bodies
// into this record; only the revision type and storage-location references are
// retained.
type RevisionSummary struct {
	RevisionType   string
	S3Bucket       string
	S3Key          string
	S3Version      string
	S3BundleType   string
	GitHubRepo     string
	GitHubCommitID string
}
