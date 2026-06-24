// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drs

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// ec2InstanceTargetType is the relationship target_type for a launched EC2
// instance backing a DRS recovery instance. Eshu does not emit an
// aws_ec2_instance resource yet, so this value is a documented forward reference
// recorded in relguard.KnownTargetTypeAllowlist; the edge keys the bare instance
// id (i-...) other scanners use to publish EC2 instance identity and leaves
// target_arn empty.
const ec2InstanceTargetType = "aws_ec2_instance"

// sourceServerRecoversToInstanceRelationship records that a DRS source server's
// reported recovery instance is the recovery instance node the scanner
// publishes. The recovery instance node publishes its resource_id as the
// recovery instance id, so the edge is keyed by that id to join the node within
// the same DRS scan. It returns nil when either endpoint identity is missing, so
// the edge never dangles.
func sourceServerRecoversToInstanceRelationship(
	boundary awscloud.Boundary,
	server SourceServer,
) *awscloud.RelationshipObservation {
	sourceID := sourceServerResourceID(server)
	recoveryInstanceID := strings.TrimSpace(server.RecoveryInstanceID)
	if sourceID == "" || recoveryInstanceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDRSSourceServerRecoversToInstance,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(server.ARN),
		TargetResourceID: recoveryInstanceID,
		TargetType:       awscloud.ResourceTypeDRSRecoveryInstance,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipDRSSourceServerRecoversToInstance + ":" + recoveryInstanceID,
	}
}

// recoveryInstanceRunsOnEC2InstanceRelationship records that a DRS recovery
// instance is backed by a launched EC2 instance. DRS reports the bare EC2
// instance id (i-...), which is exactly how EC2 instance identity is keyed as a
// relationship target across the collector, so the edge keys that bare id and
// leaves target_arn empty (no EC2 instance resource is scanned yet). It returns
// nil when either endpoint identity is missing, so the edge never dangles.
func recoveryInstanceRunsOnEC2InstanceRelationship(
	boundary awscloud.Boundary,
	instance RecoveryInstance,
) *awscloud.RelationshipObservation {
	sourceID := recoveryInstanceResourceID(instance)
	ec2InstanceID := strings.TrimSpace(instance.EC2InstanceID)
	if sourceID == "" || ec2InstanceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDRSRecoveryInstanceRunsOnEC2Instance,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(instance.ARN),
		TargetResourceID: ec2InstanceID,
		TargetType:       ec2InstanceTargetType,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipDRSRecoveryInstanceRunsOnEC2Instance + ":" + ec2InstanceID,
	}
}
