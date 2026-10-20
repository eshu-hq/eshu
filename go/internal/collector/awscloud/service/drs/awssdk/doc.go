// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Elastic Disaster Recovery (DRS)
// control-plane describe calls into the scanner-owned metadata the drs package
// consumes.
//
// The adapter's accepted SDK surface is Describe-only by construction
// (DescribeSourceServers, DescribeRecoveryInstances,
// DescribeReplicationConfigurationTemplates). It never reads replication agent
// secrets, replicated disk data, point-in-time snapshot contents, or job logs,
// and never recovers, starts, stops, or mutates DRS state. Reflection guard
// tests fail the build if a record-read, agent-read, or mutation method ever
// reaches the adapter interface.
package awssdk
