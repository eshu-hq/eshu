// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditmanager

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
)

// assessmentFrameworkRelationship records an assessment's dependency on the
// Audit Manager framework it was created from. AWS reports the framework ARN,
// which matches how the framework node publishes its resource_id. It returns nil
// when either endpoint identity is missing.
func assessmentFrameworkRelationship(
	boundary awscloud.Boundary,
	assessment Assessment,
) *awscloud.RelationshipObservation {
	sourceID := assessmentResourceID(assessment)
	targetID := firstNonEmpty(assessment.FrameworkARN, assessment.FrameworkID)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipAuditManagerAssessmentUsesFramework,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(assessment.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       catalog.ResourceTypeAuditManagerFramework,
		SourceRecordID:   sourceID + "->" + catalog.RelationshipAuditManagerAssessmentUsesFramework + ":" + targetID,
	}
}

// assessmentReportsS3Relationship records an assessment's assessment-reports S3
// destination bucket. Audit Manager reports an s3://bucket/prefix destination
// URI, so the scanner synthesizes the partition-aware bucket ARN to match the S3
// scanner's published bucket resource_id (arn:<partition>:s3:::<bucket>). It
// returns nil when no S3 destination is configured.
func assessmentReportsS3Relationship(
	boundary awscloud.Boundary,
	assessment Assessment,
) *awscloud.RelationshipObservation {
	bucket := bucketNameFromS3URI(assessment.ReportsS3Destination)
	if bucket == "" {
		return nil
	}
	sourceID := assessmentResourceID(assessment)
	if sourceID == "" {
		return nil
	}
	bucketARN := arnForBucket(awscloud.PartitionForBoundary(boundary), bucket)
	if bucketARN == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipAuditManagerAssessmentReportsToS3,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(assessment.ARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       catalog.ResourceTypeS3Bucket,
		Attributes:       map[string]any{"bucket": bucket},
		SourceRecordID:   sourceID + "->" + catalog.RelationshipAuditManagerAssessmentReportsToS3 + ":" + bucketARN,
	}
}

// assessmentKMSRelationship records the KMS key Audit Manager uses to encrypt the
// assessment's evidence and reports. Audit Manager configures one account-level
// customer managed key (GetSettings), reported as a key ARN, which matches how
// the KMS scanner publishes its key resource_id. It returns nil when no customer
// managed key is configured (AWS-owned key) or no assessment identity exists.
func assessmentKMSRelationship(
	boundary awscloud.Boundary,
	assessment Assessment,
	kmsKeyARN string,
) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(kmsKeyARN)
	if targetID == "" {
		return nil
	}
	sourceID := assessmentResourceID(assessment)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipAuditManagerAssessmentEncryptedWithKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(assessment.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       catalog.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + catalog.RelationshipAuditManagerAssessmentEncryptedWithKMSKey + ":" + targetID,
	}
}

// assessmentAccountRelationship records one AWS account included in the
// assessment scope. The target keys the partition-aware account root ARN
// (arn:<partition>:iam::<account-id>:root), the same aws_account identity the
// config, access-analyzer, and ds scanners target, so the edge does not dangle.
// It returns nil when the account id or assessment identity is missing.
func assessmentAccountRelationship(
	boundary awscloud.Boundary,
	assessment Assessment,
	accountID string,
) *awscloud.RelationshipObservation {
	accountID = strings.TrimSpace(accountID)
	sourceID := assessmentResourceID(assessment)
	if accountID == "" || sourceID == "" {
		return nil
	}
	accountARN := accountRootARN(awscloud.PartitionForBoundary(boundary), accountID)
	if accountARN == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipAuditManagerAssessmentInAccount,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(assessment.ARN),
		TargetResourceID: accountARN,
		TargetARN:        accountARN,
		TargetType:       catalog.ResourceTypeAWSAccount,
		Attributes:       map[string]any{"account_id": accountID},
		SourceRecordID:   sourceID + "->" + catalog.RelationshipAuditManagerAssessmentInAccount + ":" + accountID,
	}
}
