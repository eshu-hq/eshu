// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Kafka (Amazon MSK) client into
// the metadata-only MSK scanner interface.
//
// The adapter uses ListClustersV2, ListConfigurations, ListReplicators, and
// DescribeReplicator. It intentionally excludes CreateClusterV2,
// UpdateClusterKafkaVersion, DeleteCluster, RebootBroker, UpdateBrokerCount,
// UpdateBrokerStorage, UpdateBrokerType, UpdateConfiguration, CreateReplicator,
// DeleteReplicator, DeleteConfiguration, PutClusterPolicy, TagResource,
// UntagResource, CreateTopic, DeleteTopic, BatchAssociateScramSecret,
// BatchDisassociateScramSecret, DescribeConfigurationRevision (which would
// expose raw server.properties bodies), and GetBootstrapBrokers (which would
// expose broker endpoints).
package awssdk
