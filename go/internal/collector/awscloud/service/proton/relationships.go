// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package proton

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// environmentRoleRelationship records a Proton environment's reported Proton
// service-role dependency. AWS reports an IAM role ARN, which matches how the
// IAM scanner publishes its role resource_id, so the edge joins the role node by
// ARN. It returns nil when no service role is reported or the role identifier is
// not ARN-shaped (a non-ARN value would dangle, so no edge is keyed).
func environmentRoleRelationship(boundary awscloud.Boundary, environment Environment) *awscloud.RelationshipObservation {
	roleARN := strings.TrimSpace(environment.ProtonServiceRoleArn)
	if roleARN == "" || !isARN(roleARN) {
		return nil
	}
	sourceID := environmentResourceID(environment)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipProtonEnvironmentUsesRole,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(environment.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipProtonEnvironmentUsesRole + ":" + roleARN,
	}
}

// serviceInEnvironmentRelationship records that a Proton service is deployed
// into an environment through one of its service instances. environmentID is the
// resource_id the environment node publishes (its ARN when available), so the
// edge joins the environment node exactly. It returns nil when either endpoint
// identity is missing, so a placement that references an environment the scanner
// could not resolve never dangles.
func serviceInEnvironmentRelationship(
	boundary awscloud.Boundary,
	serviceID string,
	serviceARN string,
	environmentID string,
) *awscloud.RelationshipObservation {
	serviceID = strings.TrimSpace(serviceID)
	environmentID = strings.TrimSpace(environmentID)
	if serviceID == "" || environmentID == "" {
		return nil
	}
	targetARN := ""
	if isARN(environmentID) {
		targetARN = environmentID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipProtonServiceInEnvironment,
		SourceResourceID: serviceID,
		SourceARN:        strings.TrimSpace(serviceARN),
		TargetResourceID: environmentID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeProtonEnvironment,
		SourceRecordID:   serviceID + "->" + awscloud.RelationshipProtonServiceInEnvironment + ":" + environmentID,
	}
}
