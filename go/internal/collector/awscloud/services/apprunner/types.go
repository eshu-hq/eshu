// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apprunner

import (
	"context"
	"time"
)

// Client is the AWS App Runner read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned types. The
// surface is metadata-only: it exposes no CreateService, DeleteService,
// UpdateService, PauseService, ResumeService, StartDeployment, DeleteConnection,
// or any Create/Update/Delete/Associate operation.
type Client interface {
	ListServices(context.Context) ([]Service, error)
	ListConnections(context.Context) ([]Connection, error)
	ListAutoScalingConfigurations(context.Context) ([]AutoScalingConfiguration, error)
	ListObservabilityConfigurations(context.Context) ([]ObservabilityConfiguration, error)
	ListVpcConnectors(context.Context) ([]VpcConnector, error)
	ListVpcIngressConnections(context.Context) ([]VpcIngressConnection, error)
}

// Service is the scanner-owned representation of an App Runner service. It
// carries metadata only. Source repository credentials and runtime
// environment-variable values are intentionally absent: only environment
// variable names are kept, and secret references are carried as ARN references.
type Service struct {
	ARN                           string
	ID                            string
	Name                          string
	Status                        string
	ServiceURL                    string
	SourceConfigurationType       string
	ImageIdentifier               string
	ImageRepositoryType           string
	CodeRepositoryURL             string
	AutoDeploymentsEnabled        bool
	ConnectionARN                 string
	AccessRoleARN                 string
	InstanceRoleARN               string
	KMSKey                        string
	VpcConnectorARN               string
	EgressType                    string
	IsPubliclyAccessible          bool
	AutoScalingConfigurationARN   string
	ObservabilityEnabled          bool
	ObservabilityConfigurationARN string
	HealthCheck                   HealthCheck
	// EnvironmentVariableNames carries the runtime environment-variable keys
	// only. Values are never read or persisted.
	EnvironmentVariableNames []string
	// SecretReferences carries runtime secret references. Each value is a
	// Secrets Manager or SSM Parameter Store ARN, never the resolved value.
	SecretReferences []SecretReference
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Tags             map[string]string
}

// HealthCheck is the scanner-owned representation of an App Runner service
// health-check configuration.
type HealthCheck struct {
	Protocol           string
	Path               string
	Interval           int32
	Timeout            int32
	HealthyThreshold   int32
	UnhealthyThreshold int32
}

// SecretReference carries one App Runner runtime secret reference. Name is the
// environment-variable key the secret is bound to; ValueFrom is a Secrets
// Manager or SSM Parameter Store ARN, not the secret value.
type SecretReference struct {
	Name      string
	ValueFrom string
}

// Connection is the scanner-owned representation of an App Runner connection to
// a source code repository provider.
type Connection struct {
	ARN          string
	Name         string
	ProviderType string
	Status       string
	CreatedAt    time.Time
}

// AutoScalingConfiguration is the scanner-owned representation of an App Runner
// automatic scaling configuration revision.
type AutoScalingConfiguration struct {
	ARN            string
	Name           string
	Revision       int32
	Status         string
	IsDefault      bool
	Latest         bool
	MaxConcurrency int32
	MaxSize        int32
	MinSize        int32
	CreatedAt      time.Time
}

// ObservabilityConfiguration is the scanner-owned representation of an App
// Runner observability configuration revision.
type ObservabilityConfiguration struct {
	ARN         string
	Name        string
	Revision    int32
	Status      string
	Latest      bool
	TraceVendor string
	CreatedAt   time.Time
}

// VpcConnector is the scanner-owned representation of an App Runner VPC
// connector. Subnets and security groups are carried for EC2 network-fabric
// joins.
type VpcConnector struct {
	ARN            string
	Name           string
	Revision       int32
	Status         string
	Subnets        []string
	SecurityGroups []string
	CreatedAt      time.Time
}

// VpcIngressConnection is the scanner-owned representation of an App Runner VPC
// ingress connection.
type VpcIngressConnection struct {
	ARN           string
	Name          string
	Status        string
	DomainName    string
	ServiceARN    string
	VpcEndpointID string
	VpcID         string
	CreatedAt     time.Time
}
