// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package dax maps Amazon DynamoDB Accelerator (DAX) metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence cluster, subnet group, and parameter
// group resources plus relationships for cluster subnet-group placement, cluster
// VPC security-group membership, the cluster-assumed IAM role, subnet-group VPC
// placement, and subnet-group member subnets. Cached DynamoDB item data, query
// results, node endpoint payloads, parameter values, and mutation APIs
// (CreateCluster, DeleteCluster, UpdateCluster, IncreaseReplicationFactor,
// DecreaseReplicationFactor, RebootNode, and related mutation calls) stay
// outside this package contract: the scanner is metadata-only. DAX does not
// report a server-side-encryption KMS key ARN, so the scanner records only the
// SSE status and never synthesizes a KMS key edge.
package dax
