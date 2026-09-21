// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package resourcegroups maps AWS Resource Groups metadata into AWS cloud
// collector facts.
//
// The scanner emits one reported-confidence resource for each group (name, ARN,
// description, and query type) and the membership edges that connect a group to
// its member resources. Each member ARN is classified into its resource family,
// and the membership edge is keyed by the identity that family's own scanner
// publishes (ARN-equality for ARN-keyed families such as S3, Lambda, and
// CloudFormation stacks; the bare or prefixed id for families such as EC2,
// Elastic IP, KMS keys, and Route 53 hosted zones). Members whose family the
// classifier does not recognize are skipped rather than emitted with an empty
// target type, so no membership edge ever dangles. A
// CloudFormation-stack-backed group also emits a group-to-stack edge.
//
// The resource-query body (the tag-filter expression or CloudFormation template
// JSON) is never persisted; only the query type and, for a
// CloudFormation-stack-backed group, the stack identifier the query reports are
// kept. Mutation APIs (CreateGroup, UpdateGroup, DeleteGroup, UpdateGroupQuery,
// GroupResources, UngroupResources, Tag, Untag, PutGroupConfiguration) stay
// outside this package contract.
package resourcegroups
