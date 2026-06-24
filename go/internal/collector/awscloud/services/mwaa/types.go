// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mwaa

import (
	"context"
	"time"
)

// Client lists metadata-only Amazon MWAA observations for one claimed account
// and region. Implementations call ListEnvironments and GetEnvironment only;
// no create, update, delete, CLI-token, or web-login-token API is reachable
// through this interface.
type Client interface {
	// ListEnvironments returns the MWAA environments visible to the configured
	// AWS credentials with safe identity, network, IAM, KMS, and CloudWatch
	// Logs dependency metadata. Apache Airflow configuration option values are
	// never carried.
	ListEnvironments(ctx context.Context) ([]Environment, error)
}

// Environment is the scanner-owned Amazon MWAA environment view. It carries
// safe identity, placement, and dependency metadata. Apache Airflow
// configuration option values, connection strings, CLI tokens, web-login
// tokens, and any secret-bearing field stay outside the contract by
// construction: this type has no field that can hold them.
type Environment struct {
	// Name is the environment name (for example MyMWAAEnvironment).
	Name string
	// ARN is the environment ARN. It anchors partition derivation for every
	// outgoing edge.
	ARN string
	// Status is the AWS environment lifecycle status (for example AVAILABLE).
	Status string
	// AirflowVersion is the Apache Airflow version running on the environment.
	AirflowVersion string
	// WebserverAccessMode is the environment webserver access mode
	// (PUBLIC_ONLY or PRIVATE_ONLY).
	WebserverAccessMode string
	// EnvironmentClass is the environment class type (for example mw1.small).
	EnvironmentClass string
	// EndpointManagement records whether the environment VPC endpoints are
	// managed by the customer or by the MWAA service.
	EndpointManagement string
	// Schedulers is the configured Apache Airflow scheduler count.
	Schedulers int32
	// MinWorkers is the minimum worker count.
	MinWorkers int32
	// MaxWorkers is the maximum worker count.
	MaxWorkers int32
	// MinWebservers is the minimum webserver count.
	MinWebservers int32
	// MaxWebservers is the maximum webserver count.
	MaxWebservers int32
	// CreatedAt is the environment creation timestamp.
	CreatedAt time.Time
	// SourceBucketARN is the AWS-reported S3 bucket ARN that stores the DAGs
	// and supporting files. The scanner never reads the bucket object contents.
	SourceBucketARN string
	// ExecutionRoleARN is the IAM execution-role ARN the environment assumes.
	ExecutionRoleARN string
	// ServiceRoleARN is the service-linked role ARN, when reported.
	ServiceRoleARN string
	// KMSKey is the AWS-reported KMS key reference (ARN or key id) used to
	// encrypt environment data at rest.
	KMSKey string
	// SubnetIDs are the bare VPC subnet ids the environment is attached to.
	SubnetIDs []string
	// SecurityGroupIDs are the bare VPC security group ids the environment is
	// attached to.
	SecurityGroupIDs []string
	// LogGroups carries the per-module CloudWatch Logs log group references the
	// environment publishes Airflow logs to. Log records themselves are never
	// read.
	LogGroups []LogGroup
	// Tags are the AWS resource tags on the environment, reported verbatim.
	Tags map[string]string
}

// LogGroup is the scanner-owned reference to one Airflow log module's
// CloudWatch Logs destination. It carries the module name, the log group ARN,
// and whether the module is enabled. Log record contents are never read.
type LogGroup struct {
	// Module is the Airflow log module name (for example DagProcessingLogs).
	Module string
	// ARN is the CloudWatch Logs log group ARN the module publishes to, with
	// any trailing ":*" wildcard suffix trimmed so it matches the resource_id
	// the cloudwatchlogs scanner publishes.
	ARN string
	// Enabled reports whether the Airflow log module is enabled.
	Enabled bool
	// LogLevel is the configured Airflow log level (for example INFO). It is
	// non-secret operational metadata, not an Airflow configuration option
	// value.
	LogLevel string
}
