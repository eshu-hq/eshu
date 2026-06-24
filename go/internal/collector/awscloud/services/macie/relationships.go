// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package macie

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// memberRelationship records that a Macie member account is managed by the
// delegated administrator account (member-to-administrator). The edge targets
// the administrator account's Macie session resource so it joins the session
// node the administrator account's own scan emits. It returns false when the
// member account id is empty so a blank entry does not emit a dangling edge.
func memberRelationship(
	boundary awscloud.Boundary,
	member MemberAccount,
) (awscloud.RelationshipObservation, bool) {
	memberID := strings.TrimSpace(member.AccountID)
	if memberID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	adminID := firstNonEmpty(member.AdministratorID, boundary.AccountID)
	if adminID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipMacieMemberManagedByAdministrator,
		SourceResourceID: memberResourceID(memberID),
		TargetResourceID: sessionResourceID(adminID),
		TargetType:       awscloud.ResourceTypeMacieSession,
		Attributes: map[string]any{
			"account_id":          memberID,
			"administrator_id":    adminID,
			"relationship_status": strings.TrimSpace(member.RelationshipStatus),
		},
		SourceRecordID: memberResourceID(memberID) + "->" + sessionResourceID(adminID),
	}, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
