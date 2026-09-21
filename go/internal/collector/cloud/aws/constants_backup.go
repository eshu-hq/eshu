// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceBackup identifies the regional AWS Backup metadata-only scan slice
	// covering backup vaults, backup plans, backup selections, recovery point
	// metadata, report plans, restore testing plans, and framework metadata.
	// Recovery point contents, backup vault access policy bodies, and framework
	// control input parameter values are intentionally outside the scan slice.
	ServiceBackup = "backup"
)

const (
	// ResourceTypeBackupVault identifies an AWS Backup vault metadata resource.
	// The scanner emits identity, encryption, lock state, and recovery-point
	// count summaries; access policy JSON bodies are never persisted.
	ResourceTypeBackupVault = "aws_backup_vault"
	// ResourceTypeBackupPlan identifies an AWS Backup plan metadata resource.
	// The scanner emits identity, version, last-execution date, and rule-level
	// schedule and target-vault metadata only.
	ResourceTypeBackupPlan = "aws_backup_plan"
	// ResourceTypeBackupSelection identifies an AWS Backup selection metadata
	// resource. The scanner emits identity, the IAM role used to back up the
	// selected resources, included resource ARNs, and tag-filter conditions
	// only.
	ResourceTypeBackupSelection = "aws_backup_selection"
	// ResourceTypeBackupRecoveryPoint identifies an AWS Backup recovery point
	// metadata resource. The scanner emits identity, source resource ARN,
	// vault, status, creation/expiration time, and KMS key identity only. It
	// NEVER persists snapshot contents or recovery-point restore metadata
	// values.
	ResourceTypeBackupRecoveryPoint = "aws_backup_recovery_point"
	// ResourceTypeBackupReportPlan identifies an AWS Backup report plan
	// metadata resource. The scanner emits identity, report template, format,
	// and S3 destination metadata only.
	ResourceTypeBackupReportPlan = "aws_backup_report_plan"
	// ResourceTypeBackupRestoreTestingPlan identifies an AWS Backup restore
	// testing plan metadata resource. The scanner emits identity, schedule,
	// timezone, and last-execution timestamp only.
	ResourceTypeBackupRestoreTestingPlan = "aws_backup_restore_testing_plan"
	// ResourceTypeBackupFramework identifies an AWS Backup framework metadata
	// resource. The scanner emits identity, deployment status, and control
	// count summaries only. Framework control input parameter values are
	// never persisted because they may carry compliance-sensitive scope data.
	ResourceTypeBackupFramework = "aws_backup_framework"
	// ResourceTypeBackupFrameworkControl identifies an AWS Backup framework
	// control metadata resource. The scanner emits the control name and
	// control scope identity; control input parameter values are never
	// persisted.
	ResourceTypeBackupFrameworkControl = "aws_backup_framework_control"
)

const (
	// RelationshipBackupPlanHasSelection records membership of an AWS Backup
	// selection inside its owning backup plan.
	RelationshipBackupPlanHasSelection = "backup_plan_has_selection"
	// RelationshipBackupSelectionIncludesResource records a resource ARN that
	// an AWS Backup selection explicitly assigns to its plan.
	RelationshipBackupSelectionIncludesResource = "backup_selection_includes_resource"
	// RelationshipBackupSelectionUsesIAMRole records the IAM role an AWS Backup
	// selection uses to back up the selected resources.
	RelationshipBackupSelectionUsesIAMRole = "backup_selection_uses_iam_role"
	// RelationshipBackupVaultUsesKMSKey records the KMS key reported by an AWS
	// Backup vault's server-side encryption configuration.
	RelationshipBackupVaultUsesKMSKey = "backup_vault_uses_kms_key"
	// RelationshipBackupRecoveryPointInVault records that a recovery point is
	// stored inside its owning backup vault.
	RelationshipBackupRecoveryPointInVault = "backup_recovery_point_in_vault"
	// RelationshipBackupRecoveryPointOfResource records the source resource a
	// recovery point captured, as reported by AWS Backup metadata only.
	RelationshipBackupRecoveryPointOfResource = "backup_recovery_point_of_resource"
	// RelationshipBackupFrameworkHasControl records membership of an AWS
	// Backup framework control inside its owning framework. Control input
	// parameter values are never persisted on either side of the edge.
	RelationshipBackupFrameworkHasControl = "backup_framework_has_control"
)
