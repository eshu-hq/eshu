// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resourcegroups

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// groupContainsMemberRelationship builds the membership edge from a Resource
// Groups group to one member resource. It returns ok=false when the group ARN
// is missing or when the member's resource family is not one the classifier
// recognizes, so an unrecognized member is SKIPPED rather than emitted with an
// empty or guessed target type.
//
// The target identity matches the member family's own scanner: ARN-keyed
// families set both target_resource_id and target_arn to the member ARN;
// bare-id and prefixed-id families set only target_resource_id so the edge is
// not falsely marked ARN-keyed. The AWS-reported resource type string is kept on
// the edge attributes for transparency.
func groupContainsMemberRelationship(
	boundary awscloud.Boundary,
	group Group,
	member ResourceMember,
) (awscloud.RelationshipObservation, bool) {
	groupARN := strings.TrimSpace(group.ARN)
	if groupARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	target, ok := classifyMember(member)
	if !ok {
		return awscloud.RelationshipObservation{}, false
	}
	rel := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipResourceGroupsGroupContainsResource,
		SourceResourceID: groupARN,
		SourceARN:        groupARN,
		TargetResourceID: target.ResourceID,
		TargetType:       target.Type,
		Attributes: map[string]any{
			"member_arn":             strings.TrimSpace(member.ARN),
			"reported_resource_type": strings.TrimSpace(member.ResourceType),
		},
		SourceRecordID: groupARN + "#member#" + target.ResourceID,
	}
	if target.ARNKeyed {
		rel.TargetARN = strings.TrimSpace(member.ARN)
	}
	return rel, true
}

// groupBackedByStackRelationship builds the group-to-CloudFormation-stack edge
// for a CloudFormation-stack-backed group. It returns ok=false unless the group
// reports the CLOUDFORMATION_STACK query type, carries a stack identifier, and
// that identifier is the stack ARN the cloudformation scanner keys by. The stack
// identifier comes from the API (never a synthesized partition), so the edge
// joins the real stack node in any partition.
func groupBackedByStackRelationship(
	boundary awscloud.Boundary,
	group Group,
) (awscloud.RelationshipObservation, bool) {
	groupARN := strings.TrimSpace(group.ARN)
	if groupARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	if strings.TrimSpace(group.QueryType) != queryTypeCloudFormationStack {
		return awscloud.RelationshipObservation{}, false
	}
	stackID := strings.TrimSpace(group.StackIdentifier)
	// The cloudformation scanner keys a stack by its StackId, which is an ARN.
	// Only emit when the reported identifier is ARN-shaped so the edge joins the
	// stack node rather than dangling on a bare stack name.
	if !strings.HasPrefix(stackID, "arn:") {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipResourceGroupsGroupBackedByStack,
		SourceResourceID: groupARN,
		SourceARN:        groupARN,
		TargetResourceID: stackID,
		TargetARN:        stackID,
		TargetType:       awscloud.ResourceTypeCloudFormationStack,
		SourceRecordID:   groupARN + "#stack#" + stackID,
	}, true
}
