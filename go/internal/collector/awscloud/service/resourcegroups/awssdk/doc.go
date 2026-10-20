// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Resource Groups client into the
// metadata-only Resource Groups scanner interface.
//
// The adapter uses ListGroups, GetGroupQuery, and ListGroupResources only. For
// each group it records the query type and member resource ARNs, and for a
// CloudFormation-stack-backed group it extracts the stack identifier from the
// resource-query body. It intentionally excludes CreateGroup, UpdateGroup,
// DeleteGroup, UpdateGroupQuery, GroupResources, UngroupResources, Tag, Untag,
// PutGroupConfiguration, and any other mutation API. The resource-query body is
// never persisted beyond the stack identifier.
package awssdk
