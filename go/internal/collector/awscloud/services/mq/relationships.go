package mq

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func brokerRelationships(boundary awscloud.Boundary, broker Broker) []awscloud.RelationshipObservation {
	brokerID := firstNonEmpty(broker.ARN, broker.ID, broker.Name)
	if brokerID == "" {
		return nil
	}
	brokerARN := strings.TrimSpace(broker.ARN)
	var observations []awscloud.RelationshipObservation
	for _, subnetID := range cloneStrings(broker.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMQBrokerUsesSubnet,
			SourceResourceID: brokerID,
			SourceARN:        brokerARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(brokerID, awscloud.RelationshipMQBrokerUsesSubnet, subnetID),
		})
	}
	for _, groupID := range cloneStrings(broker.SecurityGroupIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMQBrokerUsesSecurityGroup,
			SourceResourceID: brokerID,
			SourceARN:        brokerARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(brokerID, awscloud.RelationshipMQBrokerUsesSecurityGroup, groupID),
		})
	}
	if kmsARN := strings.TrimSpace(broker.Encryption.KMSKeyID); !broker.Encryption.UseAWSOwnedKey && isARN(kmsARN) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMQBrokerUsesKMSKey,
			SourceResourceID: brokerID,
			SourceARN:        brokerARN,
			TargetResourceID: kmsARN,
			TargetARN:        kmsARN,
			TargetType:       awscloud.ResourceTypeKMSKey,
			SourceRecordID:   relationshipRecordID(brokerID, awscloud.RelationshipMQBrokerUsesKMSKey, kmsARN),
		})
	}
	if broker.Configuration != nil {
		if configID := strings.TrimSpace(broker.Configuration.ID); configID != "" {
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipMQBrokerUsesConfiguration,
				SourceResourceID: brokerID,
				SourceARN:        brokerARN,
				TargetResourceID: configID,
				TargetType:       awscloud.ResourceTypeMQConfiguration,
				Attributes: map[string]any{
					"revision": broker.Configuration.Revision,
				},
				SourceRecordID: relationshipRecordID(brokerID, awscloud.RelationshipMQBrokerUsesConfiguration, configID),
			})
		}
	}
	for _, logGroup := range brokerLogGroups(broker.Logs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMQBrokerLogsToCloudWatchLogGroup,
			SourceResourceID: brokerID,
			SourceARN:        brokerARN,
			TargetResourceID: logGroup.name,
			TargetType:       awscloud.ResourceTypeCloudWatchLogsLogGroup,
			Attributes: map[string]any{
				"log_kind": logGroup.kind,
			},
			SourceRecordID: relationshipRecordID(brokerID, awscloud.RelationshipMQBrokerLogsToCloudWatchLogGroup, logGroup.kind+":"+logGroup.name),
		})
	}
	return observations
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
