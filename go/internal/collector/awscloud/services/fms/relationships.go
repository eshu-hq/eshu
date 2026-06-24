// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fms

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// policyMemberAccountRelationship builds the applies-to-account edge from one
// Firewall Manager policy to one Organizations member account. The edge keys on
// the bare 12-digit account id with no synthesized target ARN, matching the
// resource_id the organizations scanner publishes for an
// aws_organizations_account node, so the edge joins instead of dangling. The
// relationship identity is keyed on the policy id and the account id, never on
// the member account's position in the API response.
func policyMemberAccountRelationship(
	boundary awscloud.Boundary,
	policy Policy,
	accountID string,
) (awscloud.RelationshipObservation, bool) {
	policyID := strings.TrimSpace(policy.ID)
	accountID = strings.TrimSpace(accountID)
	if policyID == "" || accountID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	sourceID := firstNonEmpty(strings.TrimSpace(policy.ARN), policyID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFMSPolicyAppliesToAccount,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(policy.ARN),
		TargetResourceID: accountID,
		TargetType:       awscloud.ResourceTypeOrganizationsAccount,
		Attributes: map[string]any{
			"security_service_type": strings.TrimSpace(policy.SecurityServiceType),
			"managed_resource_type": strings.TrimSpace(policy.ResourceType),
			"member_account_id":     accountID,
		},
		SourceRecordID: policyID + "#account#" + accountID,
	}, true
}
