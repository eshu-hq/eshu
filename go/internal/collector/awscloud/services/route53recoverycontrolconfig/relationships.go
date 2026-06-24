// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53recoverycontrolconfig

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// controlPanelInClusterRelationship records a control panel's membership in its
// owning cluster. The cluster is keyed by the cluster ARN the cluster node
// publishes as its resource_id, so the edge joins the cluster node exactly. It
// returns nil when either endpoint identity is missing.
func controlPanelInClusterRelationship(
	boundary awscloud.Boundary,
	panel ControlPanel,
) *awscloud.RelationshipObservation {
	sourceID := controlPanelResourceID(panel)
	targetID := strings.TrimSpace(panel.ClusterARN)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRoute53RecoveryControlConfigControlPanelInCluster,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(panel.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeRoute53RecoveryControlConfigCluster,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipRoute53RecoveryControlConfigControlPanelInCluster + ":" + targetID,
	}
}

// routingControlInControlPanelRelationship records a routing control's
// membership in its owning control panel. The control panel is keyed by the
// control panel ARN the panel node publishes as its resource_id. It returns nil
// when either endpoint identity is missing.
func routingControlInControlPanelRelationship(
	boundary awscloud.Boundary,
	control RoutingControl,
) *awscloud.RelationshipObservation {
	sourceID := routingControlResourceID(control)
	targetID := strings.TrimSpace(control.ControlPanelARN)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRoute53RecoveryControlConfigRoutingControlInControlPanel,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(control.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeRoute53RecoveryControlConfigControlPanel,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipRoute53RecoveryControlConfigRoutingControlInControlPanel + ":" + targetID,
	}
}

// safetyRuleInControlPanelRelationship records a safety rule's membership in the
// control panel it guards. The control panel is keyed by the control panel ARN
// the panel node publishes as its resource_id. It returns nil when either
// endpoint identity is missing.
func safetyRuleInControlPanelRelationship(
	boundary awscloud.Boundary,
	rule SafetyRule,
) *awscloud.RelationshipObservation {
	sourceID := safetyRuleResourceID(rule)
	targetID := strings.TrimSpace(rule.ControlPanelARN)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRoute53RecoveryControlConfigSafetyRuleInControlPanel,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(rule.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeRoute53RecoveryControlConfigControlPanel,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipRoute53RecoveryControlConfigSafetyRuleInControlPanel + ":" + targetID,
	}
}
