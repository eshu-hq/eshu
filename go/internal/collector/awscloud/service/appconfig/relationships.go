// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appconfig

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// environmentInApplicationRelationship records an AppConfig environment's
// membership in its owning application. applicationARN is the resource_id the
// application node publishes (its synthesized partition-aware ARN), so the edge
// joins the application node exactly. It returns nil when either endpoint
// identity is missing.
func environmentInApplicationRelationship(
	boundary awscloud.Boundary,
	environmentID string,
	applicationARN string,
) *awscloud.RelationshipObservation {
	environmentID = strings.TrimSpace(environmentID)
	applicationARN = strings.TrimSpace(applicationARN)
	if environmentID == "" || applicationARN == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppConfigEnvironmentInApplication,
		SourceResourceID: environmentID,
		SourceARN:        environmentID,
		TargetResourceID: applicationARN,
		TargetARN:        applicationARN,
		TargetType:       awscloud.ResourceTypeAppConfigApplication,
		SourceRecordID:   environmentID + "->" + awscloud.RelationshipAppConfigEnvironmentInApplication + ":" + applicationARN,
	}
}

// profileInApplicationRelationship records an AppConfig configuration profile's
// membership in its owning application. applicationARN is the resource_id the
// application node publishes. It returns nil when either endpoint identity is
// missing.
func profileInApplicationRelationship(
	boundary awscloud.Boundary,
	profileID string,
	applicationARN string,
) *awscloud.RelationshipObservation {
	profileID = strings.TrimSpace(profileID)
	applicationARN = strings.TrimSpace(applicationARN)
	if profileID == "" || applicationARN == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppConfigProfileInApplication,
		SourceResourceID: profileID,
		SourceARN:        profileID,
		TargetResourceID: applicationARN,
		TargetARN:        applicationARN,
		TargetType:       awscloud.ResourceTypeAppConfigApplication,
		SourceRecordID:   profileID + "->" + awscloud.RelationshipAppConfigProfileInApplication + ":" + applicationARN,
	}
}

// environmentMonitorsAlarmRelationship records an AppConfig environment's
// CloudWatch alarm monitor. AppConfig reports the alarm ARN, which matches how
// the CloudWatch scanner publishes its alarm resource_id (preferring the alarm
// ARN), so the edge joins the alarm node. It returns nil when no alarm ARN is
// reported or the source environment identity is missing.
func environmentMonitorsAlarmRelationship(
	boundary awscloud.Boundary,
	environmentARN string,
	monitor Monitor,
) *awscloud.RelationshipObservation {
	environmentARN = strings.TrimSpace(environmentARN)
	alarmARN := strings.TrimSpace(monitor.AlarmARN)
	if environmentARN == "" || alarmARN == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppConfigEnvironmentMonitorsAlarm,
		SourceResourceID: environmentARN,
		SourceARN:        environmentARN,
		TargetResourceID: alarmARN,
		TargetARN:        alarmARN,
		TargetType:       awscloud.ResourceTypeCloudWatchAlarm,
		SourceRecordID:   environmentARN + "->" + awscloud.RelationshipAppConfigEnvironmentMonitorsAlarm + ":" + alarmARN,
	}
}

// environmentMonitorRoleRelationship records the IAM role AppConfig assumes to
// read a monitored CloudWatch alarm. AppConfig reports a role ARN, which matches
// how the IAM scanner publishes its role resource_id. It returns nil when no
// role ARN is reported or the source environment identity is missing.
func environmentMonitorRoleRelationship(
	boundary awscloud.Boundary,
	environmentARN string,
	monitor Monitor,
) *awscloud.RelationshipObservation {
	environmentARN = strings.TrimSpace(environmentARN)
	roleARN := strings.TrimSpace(monitor.AlarmRoleARN)
	if environmentARN == "" || roleARN == "" || !isARN(roleARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppConfigEnvironmentUsesMonitorRole,
		SourceResourceID: environmentARN,
		SourceARN:        environmentARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   environmentARN + "->" + awscloud.RelationshipAppConfigEnvironmentUsesMonitorRole + ":" + roleARN,
	}
}
