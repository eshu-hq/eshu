// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCleanRooms identifies the regional AWS Clean Rooms metadata-only
	// scan slice. The scanner reads collaboration, configured-table, and
	// membership control-plane metadata through the Clean Rooms management APIs
	// (ListCollaborations, ListConfiguredTables, GetConfiguredTable,
	// ListMemberships, ListTagsForResource) and never reads or persists analysis
	// rule SQL, query bodies, allowed-column names, or member secrets, and never
	// mutates Clean Rooms state.
	ServiceCleanRooms = "cleanrooms"
)

const (
	// ResourceTypeCleanRoomsCollaboration identifies an AWS Clean Rooms
	// collaboration metadata resource. The scanner emits identity, the creator
	// account id and display name, the member status, the analytics engine, and
	// lifecycle timestamps only.
	ResourceTypeCleanRoomsCollaboration = "aws_cleanrooms_collaboration"
	// ResourceTypeCleanRoomsConfiguredTable identifies an AWS Clean Rooms
	// configured-table metadata resource. The scanner emits identity, the
	// analysis method, the configured analysis-rule type names, the count of
	// allowed columns (never the column names), and the backing-table reference
	// kind only. Analysis-rule SQL and allowed-column names stay outside the
	// contract.
	ResourceTypeCleanRoomsConfiguredTable = "aws_cleanrooms_configured_table"
	// ResourceTypeCleanRoomsMembership identifies an AWS Clean Rooms membership
	// metadata resource. The scanner emits identity, the associated
	// collaboration identity, the member abilities, and lifecycle timestamps
	// only.
	ResourceTypeCleanRoomsMembership = "aws_cleanrooms_membership"
)

const (
	// RelationshipCleanRoomsConfiguredTableUsesGlueTable records a Clean Rooms
	// configured table's backing AWS Glue Data Catalog table. Clean Rooms reports
	// the Glue database and table names, so the target is keyed by the
	// "<database>/<table>" resource_id the Glue scanner publishes for a table
	// node, joining the configured table to its Glue source.
	RelationshipCleanRoomsConfiguredTableUsesGlueTable = "cleanrooms_configured_table_uses_glue_table"
	// RelationshipCleanRoomsMembershipInCollaboration records a Clean Rooms
	// membership's association with its collaboration. The target is keyed by the
	// collaboration ARN the collaboration node publishes, so the edge joins the
	// collaboration node within the same service.
	RelationshipCleanRoomsMembershipInCollaboration = "cleanrooms_membership_in_collaboration"
)
