// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceMGN identifies the regional AWS Application Migration Service (MGN)
	// metadata-only scan slice. The scanner reads migration control-plane
	// metadata through the MGN management APIs (DescribeSourceServers,
	// ListApplications, GetLaunchConfiguration, DescribeJobs) and never reads or
	// persists replication-agent credentials, replication configuration secrets,
	// or any data-plane replication payload, and never mutates MGN state.
	ServiceMGN = "mgn"
)

const (
	// ResourceTypeMGNSourceServer identifies an AWS Application Migration Service
	// source server metadata resource. The scanner emits identity, lifecycle and
	// data-replication state, the recommended target instance type, the launched
	// cutover/test EC2 instance id, and non-secret identification hints only.
	// Replication-agent credentials and replicated disk contents stay outside the
	// contract.
	ResourceTypeMGNSourceServer = "aws_mgn_source_server"
	// ResourceTypeMGNApplication identifies an AWS Application Migration Service
	// application metadata resource. The scanner emits identity, description,
	// archival status, wave reference, and the aggregated migration status only.
	ResourceTypeMGNApplication = "aws_mgn_application"
	// ResourceTypeMGNLaunchConfiguration identifies an AWS Application Migration
	// Service launch configuration for one source server. The scanner emits the
	// launch disposition, boot mode, target-instance right-sizing method, the
	// referenced EC2 launch template id, and copy-tag/copy-private-ip flags only.
	ResourceTypeMGNLaunchConfiguration = "aws_mgn_launch_configuration"
	// ResourceTypeMGNJob identifies an AWS Application Migration Service job
	// metadata resource (a test or cutover launch run). The scanner emits the
	// job identity, type, status, initiator, and timestamps only; participating
	// server launch details beyond identity are not persisted.
	ResourceTypeMGNJob = "aws_mgn_job"
)

const (
	// RelationshipMGNApplicationContainsSourceServer records an MGN application's
	// membership of a source server. The target is keyed by the source server id
	// the source-server node publishes, so the edge joins that node exactly.
	RelationshipMGNApplicationContainsSourceServer = "mgn_application_contains_source_server"
	// RelationshipMGNSourceServerLaunchedEC2Instance records the cutover/test EC2
	// instance MGN launched for a source server. The target is keyed by the bare
	// EC2 instance id (i-...) the EC2 instance family is published under, a
	// forward reference until a dedicated EC2 instance scanner exists.
	RelationshipMGNSourceServerLaunchedEC2Instance = "mgn_source_server_launched_ec2_instance"
	// RelationshipMGNLaunchConfigurationUsesLaunchTemplate records the EC2 launch
	// template a source server's launch configuration references. The target is
	// keyed by the launch template id (lt-...) MGN reports, matching how the
	// launch-template family is published.
	RelationshipMGNLaunchConfigurationUsesLaunchTemplate = "mgn_launch_configuration_uses_launch_template"
	// RelationshipMGNJobTargetsSourceServer records that an MGN job acts on a
	// participating source server. The target is keyed by the source server id
	// the source-server node publishes, so the edge joins that node exactly.
	RelationshipMGNJobTargetsSourceServer = "mgn_job_targets_source_server"
)
