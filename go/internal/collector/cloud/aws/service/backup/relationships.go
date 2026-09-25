// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backup

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func vaultKMSRelationship(
	boundary aws.Boundary,
	vault Vault,
) (aws.RelationshipObservation, bool) {
	vaultARN := strings.TrimSpace(vault.ARN)
	kmsARN := strings.TrimSpace(vault.EncryptionKeyARN)
	if vaultARN == "" || !isKMSKeyARN(kmsARN) {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipBackupVaultUsesKMSKey,
		SourceResourceID: vaultARN,
		SourceARN:        vaultARN,
		TargetResourceID: kmsARN,
		TargetARN:        kmsARN,
		TargetType:       "aws_kms_key",
		SourceRecordID:   vaultARN + "#kms#" + kmsARN,
	}, true
}

func planHasSelectionRelationship(
	boundary aws.Boundary,
	plan Plan,
	selection Selection,
) (aws.RelationshipObservation, bool) {
	planARN := strings.TrimSpace(plan.ARN)
	selID := strings.TrimSpace(selection.ID)
	if planARN == "" || selID == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipBackupPlanHasSelection,
		SourceResourceID: planARN,
		SourceARN:        planARN,
		TargetResourceID: selID,
		TargetType:       aws.ResourceTypeBackupSelection,
		SourceRecordID:   planARN + "#selection#" + selID,
	}, true
}

func selectionRoleRelationship(
	boundary aws.Boundary,
	selection Selection,
) (aws.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(selection.IAMRoleARN)
	selID := strings.TrimSpace(selection.ID)
	if !isARN(roleARN) || selID == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipBackupSelectionUsesIAMRole,
		SourceResourceID: selID,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   selID + "#role#" + roleARN,
	}, true
}

func selectionIncludesResourceRelationship(
	boundary aws.Boundary,
	selection Selection,
	targetARN string,
) aws.RelationshipObservation {
	selID := strings.TrimSpace(selection.ID)
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipBackupSelectionIncludesResource,
		SourceResourceID: firstNonEmpty(selID, selection.Name),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       targetTypeForARN(targetARN),
		SourceRecordID:   selID + "->" + targetARN,
	}
}

func recoveryPointInVaultRelationship(
	boundary aws.Boundary,
	rp RecoveryPoint,
) (aws.RelationshipObservation, bool) {
	rpARN := strings.TrimSpace(rp.ARN)
	vaultARN := strings.TrimSpace(rp.VaultARN)
	if rpARN == "" || vaultARN == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipBackupRecoveryPointInVault,
		SourceResourceID: rpARN,
		SourceARN:        rpARN,
		TargetResourceID: vaultARN,
		TargetARN:        vaultARN,
		TargetType:       aws.ResourceTypeBackupVault,
		SourceRecordID:   rpARN + "#vault#" + vaultARN,
	}, true
}

func recoveryPointOfResourceRelationship(
	boundary aws.Boundary,
	rp RecoveryPoint,
) (aws.RelationshipObservation, bool) {
	rpARN := strings.TrimSpace(rp.ARN)
	sourceARN := strings.TrimSpace(rp.SourceResourceARN)
	if rpARN == "" || !isARN(sourceARN) {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipBackupRecoveryPointOfResource,
		SourceResourceID: rpARN,
		SourceARN:        rpARN,
		TargetResourceID: sourceARN,
		TargetARN:        sourceARN,
		TargetType:       targetTypeForARN(sourceARN),
		SourceRecordID:   rpARN + "->" + sourceARN,
	}, true
}

func frameworkHasControlRelationship(
	boundary aws.Boundary,
	framework Framework,
	control FrameworkControl,
) (aws.RelationshipObservation, bool) {
	frameworkARN := strings.TrimSpace(framework.ARN)
	controlName := strings.TrimSpace(control.Name)
	if frameworkARN == "" || controlName == "" {
		return aws.RelationshipObservation{}, false
	}
	resourceID := frameworkARN + "/" + controlName
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipBackupFrameworkHasControl,
		SourceResourceID: frameworkARN,
		SourceARN:        frameworkARN,
		TargetResourceID: resourceID,
		TargetType:       aws.ResourceTypeBackupFrameworkControl,
		SourceRecordID:   frameworkARN + "#control#" + controlName,
	}, true
}

// targetTypeForARN maps a resource ARN to its expected resource_type label
// for cross-service relationship edges. The function uses ARN service path
// segments and only returns a known type when the ARN structure matches an
// inventory category Eshu already supports; everything else falls back to
// the generic "aws_resource" target.
func targetTypeForARN(arn string) string {
	switch {
	case strings.Contains(arn, ":dynamodb:"):
		return aws.ResourceTypeDynamoDBTable
	case strings.Contains(arn, ":rds:") && strings.Contains(arn, ":cluster:"):
		return aws.ResourceTypeRDSDBCluster
	case strings.Contains(arn, ":rds:"):
		return aws.ResourceTypeRDSDBInstance
	case strings.Contains(arn, ":s3:::") || strings.HasPrefix(arn, "arn:aws:s3:::"):
		return aws.ResourceTypeS3Bucket
	case strings.Contains(arn, ":elasticache:"):
		return aws.ResourceTypeElastiCacheCacheCluster
	case strings.Contains(arn, ":redshift:"):
		return aws.ResourceTypeRedshiftCluster
	default:
		return "aws_resource"
	}
}
