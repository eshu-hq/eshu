// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mq

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// brokerRelationships builds the relationship observations for one broker.
// configurationARNs maps a broker-reported configuration ID to the ARN of the
// emitted aws_mq_configuration resource so the broker→configuration edge joins
// on the same identity the configuration scanner publishes as its ResourceID.
func brokerRelationships(boundary aws.Boundary, broker Broker, configurationARNs map[string]string) []aws.RelationshipObservation {
	brokerID := firstNonEmpty(broker.ARN, broker.ID, broker.Name)
	if brokerID == "" {
		return nil
	}
	brokerARN := strings.TrimSpace(broker.ARN)
	var observations []aws.RelationshipObservation
	for _, subnetID := range cloneStrings(broker.SubnetIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMQBrokerUsesSubnet,
			SourceResourceID: brokerID,
			SourceARN:        brokerARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(brokerID, aws.RelationshipMQBrokerUsesSubnet, subnetID),
		})
	}
	for _, groupID := range cloneStrings(broker.SecurityGroupIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMQBrokerUsesSecurityGroup,
			SourceResourceID: brokerID,
			SourceARN:        brokerARN,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(brokerID, aws.RelationshipMQBrokerUsesSecurityGroup, groupID),
		})
	}
	if kmsARN := strings.TrimSpace(broker.Encryption.KMSKeyID); !broker.Encryption.UseAWSOwnedKey && isARN(kmsARN) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMQBrokerUsesKMSKey,
			SourceResourceID: brokerID,
			SourceARN:        brokerARN,
			TargetResourceID: kmsARN,
			TargetARN:        kmsARN,
			TargetType:       aws.ResourceTypeKMSKey,
			SourceRecordID:   relationshipRecordID(brokerID, aws.RelationshipMQBrokerUsesKMSKey, kmsARN),
		})
	}
	if broker.Configuration != nil {
		if configID := strings.TrimSpace(broker.Configuration.ID); configID != "" {
			// The aws_mq_configuration resource is emitted with ResourceID set
			// to its ARN, so the edge must target the ARN to join. When the
			// referenced configuration is absent from ListConfigurations (for
			// example a shared or cross-account configuration), fall back to the
			// broker-reported ID, which the configuration resource carries as a
			// correlation anchor, rather than dropping the edge.
			configTarget := configID
			configTargetARN := ""
			if arn := strings.TrimSpace(configurationARNs[configID]); arn != "" {
				configTarget = arn
				configTargetARN = arn
			}
			observations = append(observations, aws.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: aws.RelationshipMQBrokerUsesConfiguration,
				SourceResourceID: brokerID,
				SourceARN:        brokerARN,
				TargetResourceID: configTarget,
				TargetARN:        configTargetARN,
				TargetType:       aws.ResourceTypeMQConfiguration,
				Attributes: map[string]any{
					"revision": broker.Configuration.Revision,
				},
				SourceRecordID: relationshipRecordID(brokerID, aws.RelationshipMQBrokerUsesConfiguration, configID),
			})
		}
	}
	partition := aws.PartitionFromARN(brokerARN)
	for _, logGroup := range brokerLogGroups(broker.Logs) {
		// The cloudwatchlogs scanner emits each log group with ResourceID set to
		// its non-wildcard ARN, so synthesize the matching ARN from the broker
		// partition and the boundary account and region and target it in both
		// fields, otherwise the edge cannot join the log group resource.
		logGroupARN := cloudWatchLogGroupARN(partition, boundary, logGroup.name)
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMQBrokerLogsToCloudWatchLogGroup,
			SourceResourceID: brokerID,
			SourceARN:        brokerARN,
			TargetResourceID: logGroupARN,
			TargetARN:        logGroupARN,
			TargetType:       aws.ResourceTypeCloudWatchLogsLogGroup,
			Attributes: map[string]any{
				"log_kind": logGroup.kind,
			},
			SourceRecordID: relationshipRecordID(brokerID, aws.RelationshipMQBrokerLogsToCloudWatchLogGroup, logGroup.kind+":"+logGroup.name),
		})
	}
	return observations
}

// cloudWatchLogGroupARN synthesizes the non-wildcard CloudWatch Logs log group
// ARN the cloudwatchlogs scanner publishes as its resource ResourceID, so a
// broker logging edge joins on the same identity. Amazon MQ reports broker log
// destinations as bare log group names; the ARN form is
// arn:<partition>:logs:<region>:<account>:log-group:<name>. The partition is
// taken from the broker ARN so the edge joins in the aws-us-gov and aws-cn
// partitions, not only the commercial aws partition.
func cloudWatchLogGroupARN(partition string, boundary aws.Boundary, name string) string {
	region := strings.TrimSpace(boundary.Region)
	account := strings.TrimSpace(boundary.AccountID)
	name = strings.TrimSpace(name)
	if region == "" || account == "" || name == "" {
		return name
	}
	return fmt.Sprintf("arn:%s:logs:%s:%s:log-group:%s", partition, region, account, name)
}

type brokerLogGroup struct {
	kind string
	name string
}

// brokerLogGroups returns the distinct CloudWatch Logs log group targets a
// broker reports. General and audit destinations are returned separately so a
// single broker can link to both groups, and an empty group name is skipped.
func brokerLogGroups(logs Logs) []brokerLogGroup {
	var groups []brokerLogGroup
	if name := strings.TrimSpace(logs.GeneralLogGroup); name != "" {
		groups = append(groups, brokerLogGroup{kind: "general", name: name})
	}
	if name := strings.TrimSpace(logs.AuditLogGroup); name != "" {
		groups = append(groups, brokerLogGroup{kind: "audit", name: name})
	}
	return groups
}

// relationshipRecordID encodes the relationship type into the durable
// SourceRecordID alongside the source and target identity. Including the
// relationship type keeps each relationship envelope's source ref distinct when
// a broker has multiple edges to the same target.
func relationshipRecordID(sourceID, relationshipType, targetID string) string {
	return strings.TrimSpace(sourceID) + "->" + strings.TrimSpace(relationshipType) + ":" + strings.TrimSpace(targetID)
}
