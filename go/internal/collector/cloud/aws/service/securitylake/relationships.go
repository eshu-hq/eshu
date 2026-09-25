// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securitylake

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// dataLakeS3Relationship records a data lake's backing S3 bucket. AWS reports
// the bucket ARN, which matches how the S3 scanner publishes a bucket node's
// resource_id, so the edge joins the bucket node instead of dangling. It returns
// nil when no bucket is reported.
func dataLakeS3Relationship(boundary aws.Boundary, lake DataLake) *aws.RelationshipObservation {
	bucketARN := strings.TrimSpace(lake.S3BucketARN)
	sourceID := dataLakeResourceID(lake)
	if bucketARN == "" || sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSecurityLakeDataLakeUsesS3Bucket,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(lake.ARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeS3Bucket,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSecurityLakeDataLakeUsesS3Bucket + ":" + bucketARN,
	}
}

// dataLakeKMSRelationship records a data lake's reported KMS encryption key
// dependency. AWS may report a bare key id, an alias, or an ARN; the value is
// keyed directly, matching the KMS scanner's published key resource_id
// (firstNonEmpty(keyID, keyARN)). target_arn is set only for an ARN-shaped
// value. It returns nil when no resolvable key identifier is reported.
func dataLakeKMSRelationship(boundary aws.Boundary, lake DataLake) *aws.RelationshipObservation {
	targetID := strings.TrimSpace(lake.KMSKeyID)
	sourceID := dataLakeResourceID(lake)
	if targetID == "" || sourceID == "" {
		return nil
	}
	// Security Lake reports "S3_MANAGED" (SSE-S3) or "AWS_OWNED_KMS_KEY" when no
	// customer KMS key is used; those are not resolvable KMS key resources.
	if !isResolvableKMSKey(targetID) {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSecurityLakeDataLakeUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(lake.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSecurityLakeDataLakeUsesKMSKey + ":" + targetID,
	}
}

// dataLakeLakeFormationRelationship records that a data lake registers its
// backing S3 bucket with Lake Formation. The Lake Formation scanner keys a
// registered-resource node by the registered resource ARN, which for Security
// Lake is the data lake's S3 bucket ARN, so the edge joins that node. It returns
// nil when no bucket ARN is reported.
func dataLakeLakeFormationRelationship(boundary aws.Boundary, lake DataLake) *aws.RelationshipObservation {
	bucketARN := strings.TrimSpace(lake.S3BucketARN)
	sourceID := dataLakeResourceID(lake)
	if bucketARN == "" || sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSecurityLakeDataLakeRegisteredInLakeFormation,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(lake.ARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeLakeFormationResource,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSecurityLakeDataLakeRegisteredInLakeFormation + ":" + bucketARN,
	}
}

// logSourceInDataLakeRelationship records a log source's membership in the
// Region's data lake. dataLakeID is the resource_id the data lake node
// publishes, so the edge joins the data lake node exactly. It returns nil when
// either endpoint identity is missing.
func logSourceInDataLakeRelationship(
	boundary aws.Boundary,
	dataLakeID string,
	source LogSource,
) *aws.RelationshipObservation {
	sourceID := logSourceResourceID(source)
	dataLakeID = strings.TrimSpace(dataLakeID)
	if sourceID == "" || dataLakeID == "" {
		return nil
	}
	targetARN := ""
	if isARN(dataLakeID) {
		targetARN = dataLakeID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSecurityLakeLogSourceInDataLake,
		SourceResourceID: sourceID,
		TargetResourceID: dataLakeID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeSecurityLakeDataLake,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSecurityLakeLogSourceInDataLake + ":" + dataLakeID,
	}
}

// logSourceIAMRoleRelationship records a third-party custom log source's
// log-provider IAM role. AWS reports the role ARN, which matches the IAM
// scanner's published role resource_id. It returns nil when no role ARN is
// reported (AWS-native sources never report one).
func logSourceIAMRoleRelationship(boundary aws.Boundary, source LogSource) *aws.RelationshipObservation {
	roleARN := strings.TrimSpace(source.ProviderRoleARN)
	sourceID := logSourceResourceID(source)
	if roleARN == "" || sourceID == "" {
		return nil
	}
	if !isARN(roleARN) {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSecurityLakeLogSourceUsesIAMRole,
		SourceResourceID: sourceID,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSecurityLakeLogSourceUsesIAMRole + ":" + roleARN,
	}
}

// subscriberIAMRoleRelationship records a subscriber's access IAM role. AWS
// reports the role ARN, which matches the IAM scanner's published role
// resource_id. It returns nil when no role ARN is reported.
func subscriberIAMRoleRelationship(boundary aws.Boundary, subscriber Subscriber) *aws.RelationshipObservation {
	roleARN := strings.TrimSpace(subscriber.RoleARN)
	sourceID := subscriberResourceID(subscriber)
	if roleARN == "" || sourceID == "" {
		return nil
	}
	if !isARN(roleARN) {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSecurityLakeSubscriberUsesIAMRole,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(subscriber.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSecurityLakeSubscriberUsesIAMRole + ":" + roleARN,
	}
}

// subscriberS3Relationship records a subscriber's backing S3 bucket. AWS reports
// the bucket ARN, which matches the S3 scanner's published bucket resource_id.
// It returns nil when no bucket is reported.
func subscriberS3Relationship(boundary aws.Boundary, subscriber Subscriber) *aws.RelationshipObservation {
	bucketARN := strings.TrimSpace(subscriber.S3BucketARN)
	sourceID := subscriberResourceID(subscriber)
	if bucketARN == "" || sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSecurityLakeSubscriberUsesS3Bucket,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(subscriber.ARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeS3Bucket,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSecurityLakeSubscriberUsesS3Bucket + ":" + bucketARN,
	}
}

// isResolvableKMSKey reports whether value names a customer KMS key resource the
// KMS scanner can publish, rather than a Security Lake sentinel for SSE-S3 or an
// AWS-owned key, which name no scanned resource.
func isResolvableKMSKey(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "S3_MANAGED", "AWS_OWNED_KMS_KEY", "SSE_S3":
		return false
	default:
		return true
	}
}
