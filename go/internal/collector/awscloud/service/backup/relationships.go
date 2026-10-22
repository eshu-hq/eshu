// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backup

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
)

func vaultKMSRelationship(
	boundary awscloud.Boundary,
	vault Vault,
) (awscloud.RelationshipObservation, bool) {
	vaultARN := strings.TrimSpace(vault.ARN)
	kmsARN := strings.TrimSpace(vault.EncryptionKeyARN)
	if vaultARN == "" || !isKMSKeyARN(kmsARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipBackupVaultUsesKMSKey,
		SourceResourceID: vaultARN,
		SourceARN:        vaultARN,
		TargetResourceID: kmsARN,
		TargetARN:        kmsARN,
		TargetType:       "aws_kms_key",
		SourceRecordID:   vaultARN + "#kms#" + kmsARN,
	}, true
}

func planHasSelectionRelationship(
	boundary awscloud.Boundary,
	plan Plan,
	selection Selection,
) (awscloud.RelationshipObservation, bool) {
	planARN := strings.TrimSpace(plan.ARN)
	selID := strings.TrimSpace(selection.ID)
	if planARN == "" || selID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipBackupPlanHasSelection,
		SourceResourceID: planARN,
		SourceARN:        planARN,
		TargetResourceID: selID,
		TargetType:       catalog.ResourceTypeBackupSelection,
		SourceRecordID:   planARN + "#selection#" + selID,
	}, true
}

func selectionRoleRelationship(
	boundary awscloud.Boundary,
	selection Selection,
) (awscloud.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(selection.IAMRoleARN)
	selID := strings.TrimSpace(selection.ID)
	if !isARN(roleARN) || selID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipBackupSelectionUsesIAMRole,
		SourceResourceID: selID,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       catalog.ResourceTypeIAMRole,
		SourceRecordID:   selID + "#role#" + roleARN,
	}, true
}

func selectionIncludesResourceRelationship(
	boundary awscloud.Boundary,
	selection Selection,
	targetARN string,
) awscloud.RelationshipObservation {
	selID := strings.TrimSpace(selection.ID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipBackupSelectionIncludesResource,
		SourceResourceID: firstNonEmpty(selID, selection.Name),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       targetTypeForARN(targetARN),
		SourceRecordID:   selID + "->" + targetARN,
	}
}

func recoveryPointInVaultRelationship(
	boundary awscloud.Boundary,
	rp RecoveryPoint,
) (awscloud.RelationshipObservation, bool) {
	rpARN := strings.TrimSpace(rp.ARN)
	vaultARN := strings.TrimSpace(rp.VaultARN)
	if rpARN == "" || vaultARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipBackupRecoveryPointInVault,
		SourceResourceID: rpARN,
		SourceARN:        rpARN,
		TargetResourceID: vaultARN,
		TargetARN:        vaultARN,
		TargetType:       catalog.ResourceTypeBackupVault,
		SourceRecordID:   rpARN + "#vault#" + vaultARN,
	}, true
}

func recoveryPointOfResourceRelationship(
	boundary awscloud.Boundary,
	rp RecoveryPoint,
) (awscloud.RelationshipObservation, bool) {
	rpARN := strings.TrimSpace(rp.ARN)
	sourceARN := strings.TrimSpace(rp.SourceResourceARN)
	if rpARN == "" || !isARN(sourceARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipBackupRecoveryPointOfResource,
		SourceResourceID: rpARN,
		SourceARN:        rpARN,
		TargetResourceID: sourceARN,
		TargetARN:        sourceARN,
		TargetType:       targetTypeForARN(sourceARN),
		SourceRecordID:   rpARN + "->" + sourceARN,
	}, true
}

func frameworkHasControlRelationship(
	boundary awscloud.Boundary,
	framework Framework,
	control FrameworkControl,
) (awscloud.RelationshipObservation, bool) {
	frameworkARN := strings.TrimSpace(framework.ARN)
	controlName := strings.TrimSpace(control.Name)
	if frameworkARN == "" || controlName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	resourceID := frameworkARN + "/" + controlName
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipBackupFrameworkHasControl,
		SourceResourceID: frameworkARN,
		SourceARN:        frameworkARN,
		TargetResourceID: resourceID,
		TargetType:       catalog.ResourceTypeBackupFrameworkControl,
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
		return catalog.ResourceTypeDynamoDBTable
	case strings.Contains(arn, ":rds:") && strings.Contains(arn, ":cluster:"):
		return catalog.ResourceTypeRDSDBCluster
	case strings.Contains(arn, ":rds:"):
		return catalog.ResourceTypeRDSDBInstance
	case strings.Contains(arn, ":s3:::") || strings.HasPrefix(arn, "arn:aws:s3:::"):
		return catalog.ResourceTypeS3Bucket
	case strings.Contains(arn, ":elasticache:"):
		return catalog.ResourceTypeElastiCacheCacheCluster
	case strings.Contains(arn, ":redshift:"):
		return catalog.ResourceTypeRedshiftCluster
	default:
		return "aws_resource"
	}
}
