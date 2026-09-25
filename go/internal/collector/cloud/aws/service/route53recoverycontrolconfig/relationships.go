// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53recoverycontrolconfig

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// controlPanelInClusterRelationship records a control panel's membership in its
// owning cluster. The cluster is keyed by the cluster ARN the cluster node
// publishes as its resource_id, so the edge joins the cluster node exactly. It
// returns nil when either endpoint identity is missing.
func controlPanelInClusterRelationship(
	boundary aws.Boundary,
	panel ControlPanel,
) *aws.RelationshipObservation {
	sourceID := controlPanelResourceID(panel)
	targetID := strings.TrimSpace(panel.ClusterARN)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipRoute53RecoveryControlConfigControlPanelInCluster,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(panel.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeRoute53RecoveryControlConfigCluster,
		SourceRecordID: sourceID + "->" +
			aws.RelationshipRoute53RecoveryControlConfigControlPanelInCluster + ":" + targetID,
	}
}

// routingControlInControlPanelRelationship records a routing control's
// membership in its owning control panel. The control panel is keyed by the
// control panel ARN the panel node publishes as its resource_id. It returns nil
// when either endpoint identity is missing.
func routingControlInControlPanelRelationship(
	boundary aws.Boundary,
	control RoutingControl,
) *aws.RelationshipObservation {
	sourceID := routingControlResourceID(control)
	targetID := strings.TrimSpace(control.ControlPanelARN)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipRoute53RecoveryControlConfigRoutingControlInControlPanel,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(control.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeRoute53RecoveryControlConfigControlPanel,
		SourceRecordID: sourceID + "->" +
			aws.RelationshipRoute53RecoveryControlConfigRoutingControlInControlPanel + ":" + targetID,
	}
}

// safetyRuleInControlPanelRelationship records a safety rule's membership in the
// control panel it guards. The control panel is keyed by the control panel ARN
// the panel node publishes as its resource_id. It returns nil when either
// endpoint identity is missing.
func safetyRuleInControlPanelRelationship(
	boundary aws.Boundary,
	rule SafetyRule,
) *aws.RelationshipObservation {
	sourceID := safetyRuleResourceID(rule)
	targetID := strings.TrimSpace(rule.ControlPanelARN)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipRoute53RecoveryControlConfigSafetyRuleInControlPanel,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(rule.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeRoute53RecoveryControlConfigControlPanel,
		SourceRecordID: sourceID + "->" +
			aws.RelationshipRoute53RecoveryControlConfigSafetyRuleInControlPanel + ":" + targetID,
	}
}
