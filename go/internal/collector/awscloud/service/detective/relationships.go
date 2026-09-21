// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detective

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// memberAccountRelationship builds the graph-to-member-account edge. It targets
// the AWS Organizations account node by the bare 12-digit account id, which is
// exactly the resource id the organizations scanner publishes for an account, so
// the edge joins rather than dangles. The edge is omitted when the account id is
// blank.
func memberAccountRelationship(
	boundary awscloud.Boundary,
	graphARN string,
	member MemberAccount,
) (awscloud.RelationshipObservation, bool) {
	accountID := strings.TrimSpace(member.AccountID)
	if graphARN == "" || accountID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDetectiveGraphHasMemberAccount,
		SourceResourceID: graphARN,
		SourceARN:        graphARN,
		TargetResourceID: accountID,
		TargetType:       awscloud.ResourceTypeOrganizationsAccount,
		Attributes: map[string]any{
			"account_id":        accountID,
			"membership_status": strings.TrimSpace(member.Status),
			"invitation_type":   strings.TrimSpace(member.InvitationType),
		},
		SourceRecordID: graphARN + "#member#" + accountID,
	}, true
}

// guardDutyDetectorRelationship builds the graph-to-GuardDuty-detector edge. It
// targets the GuardDuty detector node by the bare detector id, which is exactly
// the resource id the guardduty scanner publishes for a detector. Detective's
// metadata APIs never report a detector id, so the edge is emitted only when a
// resolver supplied a real id on the graph (Graph.GuardDutyDetectorID); a blank
// id yields no edge, never a fabricated one, so the edge can never dangle.
func guardDutyDetectorRelationship(
	boundary awscloud.Boundary,
	graph Graph,
) (awscloud.RelationshipObservation, bool) {
	graphARN := strings.TrimSpace(graph.ARN)
	detectorID := strings.TrimSpace(graph.GuardDutyDetectorID)
	if graphARN == "" || detectorID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDetectiveGraphSourcesGuardDutyDetector,
		SourceResourceID: graphARN,
		SourceARN:        graphARN,
		TargetResourceID: detectorID,
		TargetType:       awscloud.ResourceTypeGuardDutyDetector,
		Attributes: map[string]any{
			"detector_id": detectorID,
		},
		SourceRecordID: graphARN + "#guardduty-detector#" + detectorID,
	}, true
}
