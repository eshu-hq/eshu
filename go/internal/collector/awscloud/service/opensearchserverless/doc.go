// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package opensearchserverless maps Amazon OpenSearch Serverless (aoss)
// collection, security-policy, and managed VPC-endpoint metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence resources for OpenSearch Serverless
// collections, security policies, and managed interface VPC endpoints, plus
// relationships for collection-to-KMS-key (resolved from the matching encryption
// security policy) and VPC-endpoint-to-VPC/subnet/security-group evidence. The
// OpenSearch HTTP data plane (index, search, bulk, document APIs), access-policy
// and security-policy document bodies, collection and dashboard endpoints, and
// any mutation API stay outside this package contract: the scanner is
// metadata-only.
//
// It is a distinct service_kind from the opensearch scanner, which surfaces
// Serverless collections only as a side slice of the OpenSearch Service domain
// scan. This scanner is the first-class Serverless owner that additionally emits
// security policies as resources and the managed VPC endpoint's network edges.
package opensearchserverless
