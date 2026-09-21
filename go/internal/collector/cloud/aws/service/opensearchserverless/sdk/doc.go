// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 OpenSearch Serverless (aoss)
// control-plane calls into the scanner-owned metadata model.
//
// The adapter reads collections (ListCollections, BatchGetCollection), security
// policies (ListSecurityPolicies, GetSecurityPolicy), managed VPC endpoints
// (ListVpcEndpoints, BatchGetVpcEndpoint), and resource tags
// (ListTagsForResource) only. It parses the customer-managed KMS key ARN and
// collection resource patterns out of encryption policy bodies to key
// collection-to-KMS edges, then discards the policy body. It never reaches the
// OpenSearch HTTP data plane, never persists access-policy or security-policy
// document bodies, and never calls a Create/Update/Delete mutation API; a
// reflective exclusion test enforces that surface at build time.
package awssdk
