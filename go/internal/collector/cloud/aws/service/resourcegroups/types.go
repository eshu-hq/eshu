// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resourcegroups

import (
	"context"
	"time"
)

// Client is the AWS Resource Groups metadata read surface consumed by Scanner.
// Runtime adapters translate AWS SDK responses into scanner-owned metadata
// records.
//
// The interface is intentionally narrow: it covers only the inventory, query
// type, and membership reads Eshu needs. It must NEVER expose CreateGroup,
// UpdateGroup, DeleteGroup, UpdateGroupQuery, GroupResources, UngroupResources,
// Tag, Untag, PutGroupConfiguration, or any other mutation operation, nor must
// it surface the resource-query body. Tests assert the surface by asserting
// only the listed methods are present on the interface.
type Client interface {
	// ListGroups returns the Resource Groups groups owned in the claimed
	// boundary, each already enriched with its query type and member resources.
	ListGroups(context.Context) ([]Group, error)
}

// Group is the metadata-only scanner view of one AWS Resource Groups group.
//
// The resource-query body (the tag-filter expression or CloudFormation
// template JSON) is intentionally outside the contract: only the query type is
// recorded, because the query body can encode tag keys and values that are not
// needed to type the membership edges. CreationTime is recorded when the API
// reports it.
type Group struct {
	// ARN is the group's Amazon Resource Name as reported by the API. The
	// partition is taken from this ARN, never synthesized, so GovCloud and China
	// groups keep their real partition.
	ARN string
	// Name is the group name.
	Name string
	// Description is the optional group description.
	Description string
	// QueryType is the group's resource-query type, one of TAG_FILTERS_1_0 or
	// CLOUDFORMATION_STACK_1_0. It is the only part of the query the scanner
	// records.
	QueryType string
	// StackIdentifier is the CloudFormation stack ARN (or name) a
	// CloudFormation-stack-backed group tracks, when the API reports it on the
	// query. It is empty for tag-filter groups.
	StackIdentifier string
	// CreationTime is when the group was created, when the API reports it.
	CreationTime time.Time
	// Members are the resources the ListGroupResources API reports as belonging
	// to the group.
	Members []ResourceMember
}

// ResourceMember is the metadata-only scanner view of one resource the
// ListGroupResources API reports as a member of a Resource Groups group. Only
// the member's ARN and the AWS-reported resource type string are recorded; no
// member resource attributes, tags, or contents are read.
type ResourceMember struct {
	// ARN is the member resource's Amazon Resource Name as reported by the API.
	// It is the only identity the membership classifier needs.
	ARN string
	// ResourceType is the AWS-reported resource type string, for example
	// AWS::EC2::Instance. It is preserved on the edge attributes for
	// transparency, but the membership target type is derived from the ARN
	// family, not from this string.
	ResourceType string
}
