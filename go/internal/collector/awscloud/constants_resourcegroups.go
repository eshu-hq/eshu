// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceResourceGroups identifies the regional AWS Resource Groups
	// metadata-only scan slice covering resource groups, their query type
	// (tag-filter or CloudFormation-stack backed), and the membership edges that
	// connect each group to its member resources. Group query bodies (the tag
	// filter or CloudFormation template JSON) are never persisted; only the query
	// type and the member resource ARNs the API reports are recorded.
	ServiceResourceGroups = "resourcegroups"
)

const (
	// ResourceTypeResourceGroupsGroup identifies an AWS Resource Groups group
	// metadata resource. The scanner emits identity (name, ARN, description) and
	// the group's query type only; the resource-query body is never persisted.
	ResourceTypeResourceGroupsGroup = "aws_resourcegroups_group"
)

const (
	// RelationshipResourceGroupsGroupContainsResource records that a Resource
	// Groups group counts a member resource as part of the group, as reported by
	// the ListGroupResources API. The target is typed from the member resource's
	// ARN family and keyed by the identity that family's scanner publishes
	// (ARN-equality for ARN-keyed families, bare or prefixed id otherwise). The
	// edge is only emitted when the member family resolves to a declared resource
	// type; members of an unrecognized family are skipped rather than emitted with
	// an empty target type.
	RelationshipResourceGroupsGroupContainsResource = "resourcegroups_group_contains_resource"
	// RelationshipResourceGroupsGroupBackedByStack records that a Resource Groups
	// group is backed by a CloudFormation stack (the group's query type is
	// CLOUDFORMATION_STACK_1_0). The target is the CloudFormation stack the group
	// tracks, keyed by the stack ARN the cloudformation scanner publishes.
	RelationshipResourceGroupsGroupBackedByStack = "resourcegroups_group_backed_by_stack"
)
