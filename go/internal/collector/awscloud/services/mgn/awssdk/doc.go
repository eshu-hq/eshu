// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Application Migration Service
// (MGN) client into the metadata-only MGN scanner interface.
//
// The adapter uses DescribeSourceServers, ListApplications,
// GetLaunchConfiguration, and DescribeJobs to read MGN migration control-plane
// metadata. It intentionally excludes GetReplicationConfiguration and the
// replication-configuration-template reads (which carry staging credentials),
// every Create/Update/Delete/Start/Stop/Terminate/Mark mutation API, and the
// replication-agent and connector control APIs, so the adapter cannot read
// replication secrets or write MGN state.
package awssdk
