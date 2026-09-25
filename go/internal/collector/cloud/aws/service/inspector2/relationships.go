// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inspector2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// memberRelationship records that a member account is managed by the delegated
// administrator account (member-to-administrator).
func memberRelationship(
	boundary aws.Boundary,
	member MemberAccount,
) aws.RelationshipObservation {
	memberID := strings.TrimSpace(member.AccountID)
	adminID := firstNonEmpty(member.AdministratorID, boundary.AccountID)
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipInspector2MemberManagedByAdministrator,
		SourceResourceID: memberResourceID(memberID),
		TargetResourceID: accountResourceID(adminID),
		TargetType:       aws.ResourceTypeInspector2Account,
		Attributes: map[string]any{
			"account_id":          memberID,
			"administrator_id":    adminID,
			"relationship_status": strings.TrimSpace(member.RelationshipStatus),
		},
		SourceRecordID: memberResourceID(memberID) + "->" + accountResourceID(adminID),
	}
}

// cisTargetRelationship records that a CIS scan configuration targets one
// member account (CIS-config-to-target-account-set). It returns false for an
// empty account id so target lists with blanks do not emit dangling edges.
func cisTargetRelationship(
	boundary aws.Boundary,
	config CisScanConfiguration,
	targetAccount string,
) (aws.RelationshipObservation, bool) {
	configARN := strings.TrimSpace(config.ARN)
	accountID := strings.TrimSpace(targetAccount)
	if configARN == "" || accountID == "" {
		return aws.RelationshipObservation{}, false
	}
	configID := firstNonEmpty(configARN, config.Name)
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipInspector2CisScanConfigurationTargetsAccount,
		SourceResourceID: configID,
		SourceARN:        configARN,
		TargetResourceID: accountResourceID(accountID),
		TargetType:       aws.ResourceTypeInspector2Account,
		Attributes: map[string]any{
			"target_account_id": accountID,
		},
		SourceRecordID: configID + "->" + accountResourceID(accountID),
	}, true
}
