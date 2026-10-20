// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cleanrooms

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// configuredTableGlueRelationship records a Clean Rooms configured table's
// backing AWS Glue Data Catalog table. Clean Rooms reports the Glue database and
// table names, so the target is keyed by the "<database>/<table>" resource_id
// the Glue scanner publishes for a table node, joining the configured table to
// its Glue source. When the database name is missing it falls back to the
// "<table>" resource_id, matching the Glue scanner's own table-node fallback so
// the edge still joins. It returns nil when the backing table is not a Glue
// table or when the Glue table name is missing, so the edge is skipped rather
// than dangled.
func configuredTableGlueRelationship(
	boundary awscloud.Boundary,
	table ConfiguredTable,
) *awscloud.RelationshipObservation {
	if !strings.EqualFold(strings.TrimSpace(table.TableReferenceKind), "glue") {
		return nil
	}
	targetID := glueTableResourceID(table.GlueDatabaseName, table.GlueTableName)
	if targetID == "" {
		return nil
	}
	sourceID := configuredTableResourceID(table)
	if sourceID == "" {
		return nil
	}
	attributes := map[string]any{
		"glue_database_name": strings.TrimSpace(table.GlueDatabaseName),
		"glue_table_name":    strings.TrimSpace(table.GlueTableName),
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCleanRoomsConfiguredTableUsesGlueTable,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(table.ARN),
		TargetResourceID: targetID,
		// The Glue table node publishes a "<database>/<table>" resource_id, not an
		// ARN, so target_arn is left empty.
		TargetType:     awscloud.ResourceTypeGlueTable,
		Attributes:     attributes,
		SourceRecordID: sourceID + "->" + awscloud.RelationshipCleanRoomsConfiguredTableUsesGlueTable + ":" + targetID,
	}
}

// membershipCollaborationRelationship records a Clean Rooms membership's
// association with its collaboration. The membership summary reports the
// collaboration ARN, which matches how the collaboration node publishes its
// resource_id, so the internal edge joins the collaboration node exactly. It
// returns nil when either endpoint identity is missing.
func membershipCollaborationRelationship(
	boundary awscloud.Boundary,
	membership Membership,
) *awscloud.RelationshipObservation {
	sourceID := membershipResourceID(membership)
	targetID := strings.TrimSpace(membership.CollaborationARN)
	if targetID == "" {
		targetID = strings.TrimSpace(membership.CollaborationID)
	}
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	attributes := map[string]any{}
	if id := strings.TrimSpace(membership.CollaborationID); id != "" {
		attributes["collaboration_id"] = id
	}
	if name := strings.TrimSpace(membership.CollaborationName); name != "" {
		attributes["collaboration_name"] = name
	}
	if len(attributes) == 0 {
		attributes = nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCleanRoomsMembershipInCollaboration,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(membership.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeCleanRoomsCollaboration,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipCleanRoomsMembershipInCollaboration + ":" + targetID,
	}
}
