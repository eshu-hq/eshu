// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backup

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// vaultObservation projects one AWS Backup vault into a metadata-only
// resource observation. The function records identity, encryption metadata,
// lock state, recovery point count, and a boolean signalling whether the
// vault has an access policy attached. The access policy body itself is
// NEVER persisted because it encodes cross-account trust.
func vaultObservation(boundary awscloud.Boundary, vault Vault) awscloud.ResourceObservation {
	vaultARN := strings.TrimSpace(vault.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          vaultARN,
		ResourceID:   firstNonEmpty(vaultARN, vault.Name),
		ResourceType: awscloud.ResourceTypeBackupVault,
		Name:         strings.TrimSpace(vault.Name),
		Attributes: map[string]any{
			"encryption_key_arn":        strings.TrimSpace(vault.EncryptionKeyARN),
			"encryption_key_type":       strings.TrimSpace(vault.EncryptionKeyType),
			"locked":                    vault.Locked,
			"lock_date":                 timeOrNil(vault.LockDate),
			"min_retention_days":        int64OrNil(vault.MinRetentionDays),
			"max_retention_days":        int64OrNil(vault.MaxRetentionDays),
			"number_of_recovery_points": vault.NumberOfRecoveryPoints,
			"creation_date":             timeOrNil(vault.CreationDate),
			"has_access_policy":         vault.HasAccessPolicy,
		},
		CorrelationAnchors: []string{vaultARN, vault.Name},
		SourceRecordID:     firstNonEmpty(vaultARN, vault.Name),
	}
}

func planObservation(boundary awscloud.Boundary, plan Plan) awscloud.ResourceObservation {
	planARN := strings.TrimSpace(plan.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          planARN,
		ResourceID:   firstNonEmpty(planARN, plan.ID, plan.Name),
		ResourceType: awscloud.ResourceTypeBackupPlan,
		Name:         strings.TrimSpace(plan.Name),
		Attributes: map[string]any{
			"plan_id":             strings.TrimSpace(plan.ID),
			"version_id":          strings.TrimSpace(plan.VersionID),
			"creation_date":       timeOrNil(plan.CreationDate),
			"last_execution_date": timeOrNil(plan.LastExecutionDate),
			"rules":               planRuleNodes(plan.Rules),
		},
		CorrelationAnchors: []string{planARN, plan.ID, plan.Name},
		SourceRecordID:     firstNonEmpty(planARN, plan.ID, plan.Name),
	}
}

func planRuleNodes(rules []PlanRule) []map[string]any {
	if len(rules) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, map[string]any{
			"name":                      strings.TrimSpace(rule.Name),
			"schedule_expression":       strings.TrimSpace(rule.ScheduleExpression),
			"target_backup_vault_name":  strings.TrimSpace(rule.TargetBackupVaultName),
			"start_window_minutes":      int64OrNil(rule.StartWindowMinutes),
			"completion_window_minutes": int64OrNil(rule.CompletionWindowMinutes),
			"enable_continuous_backup":  boolOrNil(rule.EnableContinuousBackup),
		})
	}
	return out
}

func selectionObservation(
	boundary awscloud.Boundary,
	selection Selection,
) awscloud.ResourceObservation {
	selID := strings.TrimSpace(selection.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   firstNonEmpty(selID, selection.Name),
		ResourceType: awscloud.ResourceTypeBackupSelection,
		Name:         strings.TrimSpace(selection.Name),
		Attributes: map[string]any{
			"selection_id":   selID,
			"plan_id":        strings.TrimSpace(selection.PlanID),
			"iam_role_arn":   strings.TrimSpace(selection.IAMRoleARN),
			"resources":      cloneStrings(selection.Resources),
			"not_resources":  cloneStrings(selection.NotResources),
			"tag_conditions": tagConditionNodes(selection.TagConditions),
			"creation_date":  timeOrNil(selection.CreationDate),
		},
		CorrelationAnchors: []string{selID, selection.Name},
		SourceRecordID:     firstNonEmpty(selID, selection.Name),
	}
}

func tagConditionNodes(conditions []TagCondition) []map[string]any {
	if len(conditions) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(conditions))
	for _, cond := range conditions {
		out = append(out, map[string]any{
			"operator": strings.TrimSpace(cond.Operator),
			"key":      strings.TrimSpace(cond.Key),
			"value":    strings.TrimSpace(cond.Value),
		})
	}
	return out
}

// recoveryPointObservation projects one recovery point into a metadata-only
// resource observation. Snapshot contents and recovery-point restore metadata
// values are NEVER read; only identity and timing metadata is persisted.
func recoveryPointObservation(
	boundary awscloud.Boundary,
	rp RecoveryPoint,
) awscloud.ResourceObservation {
	rpARN := strings.TrimSpace(rp.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          rpARN,
		ResourceID:   rpARN,
		ResourceType: awscloud.ResourceTypeBackupRecoveryPoint,
		State:        strings.TrimSpace(rp.Status),
		Attributes: map[string]any{
			"vault_name":           strings.TrimSpace(rp.VaultName),
			"vault_arn":            strings.TrimSpace(rp.VaultARN),
			"source_resource_arn":  strings.TrimSpace(rp.SourceResourceARN),
			"source_resource_type": strings.TrimSpace(rp.SourceResourceType),
			"status":               strings.TrimSpace(rp.Status),
			"status_message":       strings.TrimSpace(rp.StatusMessage),
			"encryption_key_arn":   strings.TrimSpace(rp.EncryptionKeyARN),
			"is_encrypted":         rp.IsEncrypted,
			"creation_date":        timeOrNil(rp.CreationDate),
			"completion_date":      timeOrNil(rp.CompletionDate),
			"calculated_delete_at": timeOrNil(rp.CalculatedDeleteAt),
			"backup_size_in_bytes": int64OrNil(rp.BackupSizeInBytes),
			"storage_class":        strings.TrimSpace(rp.StorageClass),
		},
		CorrelationAnchors: []string{rpARN},
		SourceRecordID:     rpARN,
	}
}

func reportPlanObservation(
	boundary awscloud.Boundary,
	plan ReportPlan,
) awscloud.ResourceObservation {
	planARN := strings.TrimSpace(plan.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          planARN,
		ResourceID:   firstNonEmpty(planARN, plan.Name),
		ResourceType: awscloud.ResourceTypeBackupReportPlan,
		Name:         strings.TrimSpace(plan.Name),
		Attributes: map[string]any{
			"deployment_status":              strings.TrimSpace(plan.DeploymentStatus),
			"report_template":                strings.TrimSpace(plan.ReportTemplate),
			"formats":                        cloneStrings(plan.Formats),
			"s3_bucket_name":                 strings.TrimSpace(plan.S3BucketName),
			"s3_key_prefix":                  strings.TrimSpace(plan.S3KeyPrefix),
			"creation_time":                  timeOrNil(plan.CreationTime),
			"last_attempted_execution_time":  timeOrNil(plan.LastAttemptedExecutionTime),
			"last_successful_execution_time": timeOrNil(plan.LastSuccessfulExecutionTime),
		},
		CorrelationAnchors: []string{planARN, plan.Name},
		SourceRecordID:     firstNonEmpty(planARN, plan.Name),
	}
}

func restoreTestingPlanObservation(
	boundary awscloud.Boundary,
	plan RestoreTestingPlan,
) awscloud.ResourceObservation {
	planARN := strings.TrimSpace(plan.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          planARN,
		ResourceID:   firstNonEmpty(planARN, plan.Name),
		ResourceType: awscloud.ResourceTypeBackupRestoreTestingPlan,
		Name:         strings.TrimSpace(plan.Name),
		Attributes: map[string]any{
			"schedule_expression": strings.TrimSpace(plan.ScheduleExpression),
			"schedule_timezone":   strings.TrimSpace(plan.ScheduleTimezone),
			"creation_time":       timeOrNil(plan.CreationTime),
			"last_execution_time": timeOrNil(plan.LastExecutionTime),
			"last_update_time":    timeOrNil(plan.LastUpdateTime),
		},
		CorrelationAnchors: []string{planARN, plan.Name},
		SourceRecordID:     firstNonEmpty(planARN, plan.Name),
	}
}

// frameworkObservation projects one framework into a metadata-only resource
// observation. Framework control input parameter values are NEVER persisted
// because they may carry compliance-sensitive scope data.
func frameworkObservation(
	boundary awscloud.Boundary,
	framework Framework,
) awscloud.ResourceObservation {
	frameworkARN := strings.TrimSpace(framework.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          frameworkARN,
		ResourceID:   firstNonEmpty(frameworkARN, framework.Name),
		ResourceType: awscloud.ResourceTypeBackupFramework,
		Name:         strings.TrimSpace(framework.Name),
		Attributes: map[string]any{
			"description":        strings.TrimSpace(framework.Description),
			"deployment_status":  strings.TrimSpace(framework.DeploymentStatus),
			"number_of_controls": framework.NumberOfControls,
			"creation_time":      timeOrNil(framework.CreationTime),
		},
		CorrelationAnchors: []string{frameworkARN, framework.Name},
		SourceRecordID:     firstNonEmpty(frameworkARN, framework.Name),
	}
}

func frameworkControlObservation(
	boundary awscloud.Boundary,
	framework Framework,
	control FrameworkControl,
) awscloud.ResourceObservation {
	frameworkARN := strings.TrimSpace(framework.ARN)
	controlName := strings.TrimSpace(control.Name)
	resourceID := frameworkARN + "/" + controlName
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeBackupFrameworkControl,
		Name:         controlName,
		Attributes: map[string]any{
			"control_name":              controlName,
			"framework_arn":             frameworkARN,
			"framework_name":            strings.TrimSpace(framework.Name),
			"scope_compliance_types":    cloneStrings(control.ScopeComplianceTypes),
			"scope_tag_keys":            cloneStrings(control.ScopeTagKeys),
			"scope_resource_type_count": control.ScopeResourceTypeCount,
		},
		CorrelationAnchors: []string{resourceID, controlName},
		SourceRecordID:     resourceID,
	}
}
