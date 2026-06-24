// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mgn

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// ec2InstanceTargetType is the target_type for the source-server-launched-EC2
// edge. It mirrors the value other scanners use and the relguard
// KnownTargetTypeAllowlist forward-reference entry "aws_ec2_instance"; no EC2
// instance resource scanner exists yet, so the edge is a documented forward
// reference keyed by the bare instance id.
const ec2InstanceTargetType = "aws_ec2_instance"

// applicationContainsSourceServerRelationship records an MGN application's
// membership of a source server. MGN reports the application id on the source
// server, and the application node publishes its application id as resource_id,
// so the edge joins the application node by that id. It returns nil when either
// endpoint identity is missing.
func applicationContainsSourceServerRelationship(
	boundary awscloud.Boundary,
	server SourceServer,
) *awscloud.RelationshipObservation {
	applicationID := strings.TrimSpace(server.ApplicationID)
	sourceID := sourceServerResourceID(server)
	if applicationID == "" || sourceID == "" {
		return nil
	}
	// The source-server node publishes its resource_id as the bare MGN source
	// server id, so the target join key is that bare id and target_arn stays
	// empty (relguard rejects a bare join key alongside a populated ARN).
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipMGNApplicationContainsSourceServer,
		SourceResourceID: applicationID,
		TargetResourceID: sourceID,
		TargetType:       awscloud.ResourceTypeMGNSourceServer,
		SourceRecordID:   applicationID + "->" + awscloud.RelationshipMGNApplicationContainsSourceServer + ":" + sourceID,
	}
}

// sourceServerLaunchedEC2Relationship records the cutover/test EC2 instance MGN
// launched for a source server. MGN reports a bare EC2 instance id (i-...),
// which is how the EC2 instance family is keyed, so the target join key is the
// bare id and target_arn stays empty (relguard rejects a bare join key with a
// populated ARN). It returns nil when no launched instance is reported.
func sourceServerLaunchedEC2Relationship(
	boundary awscloud.Boundary,
	server SourceServer,
) *awscloud.RelationshipObservation {
	instanceID := strings.TrimSpace(server.LaunchedEC2InstanceID)
	if !isInstanceID(instanceID) {
		return nil
	}
	sourceID := sourceServerResourceID(server)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipMGNSourceServerLaunchedEC2Instance,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(server.ARN),
		TargetResourceID: instanceID,
		TargetType:       ec2InstanceTargetType,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipMGNSourceServerLaunchedEC2Instance + ":" + instanceID,
	}
}

// launchConfigurationUsesLaunchTemplateRelationship records the EC2 launch
// template a source server's launch configuration references. MGN reports a
// bare launch template id (lt-...), the form the launch-template family is keyed
// under, so the target join key is the bare id and target_arn stays empty. It
// returns nil when the launch configuration references no launch template.
func launchConfigurationUsesLaunchTemplateRelationship(
	boundary awscloud.Boundary,
	server SourceServer,
) *awscloud.RelationshipObservation {
	config := server.LaunchConfiguration
	if config == nil {
		return nil
	}
	templateID := strings.TrimSpace(config.EC2LaunchTemplateID)
	if !isLaunchTemplateID(templateID) {
		return nil
	}
	sourceID := launchConfigurationResourceID(server.SourceServerID)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipMGNLaunchConfigurationUsesLaunchTemplate,
		SourceResourceID: sourceID,
		TargetResourceID: templateID,
		TargetType:       awscloud.ResourceTypeEC2LaunchTemplate,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipMGNLaunchConfigurationUsesLaunchTemplate + ":" + templateID,
	}
}

// jobTargetsSourceServerRelationships records the participating source servers
// an MGN job acted on. Each target is keyed by the source server id the
// source-server node publishes, so the edges join those nodes. Duplicate
// participating ids produce one edge. Unknown or empty ids are skipped.
func jobTargetsSourceServerRelationships(
	boundary awscloud.Boundary,
	job Job,
) []awscloud.RelationshipObservation {
	jobID := jobResourceID(job)
	if jobID == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var relationships []awscloud.RelationshipObservation
	for _, serverID := range job.ParticipatingSourceServerIDs {
		serverID = strings.TrimSpace(serverID)
		if serverID == "" {
			continue
		}
		if _, exists := seen[serverID]; exists {
			continue
		}
		seen[serverID] = struct{}{}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMGNJobTargetsSourceServer,
			SourceResourceID: jobID,
			SourceARN:        strings.TrimSpace(job.ARN),
			TargetResourceID: serverID,
			TargetType:       awscloud.ResourceTypeMGNSourceServer,
			SourceRecordID:   jobID + "->" + awscloud.RelationshipMGNJobTargetsSourceServer + ":" + serverID,
		})
	}
	return relationships
}
