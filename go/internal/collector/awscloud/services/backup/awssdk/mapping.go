// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbackup "github.com/aws/aws-sdk-go-v2/service/backup"
	awsbackuptypes "github.com/aws/aws-sdk-go-v2/service/backup/types"

	backupservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/backup"
)

func mapVaultListMember(item awsbackuptypes.BackupVaultListMember) backupservice.Vault {
	vault := backupservice.Vault{
		ARN:              strings.TrimSpace(aws.ToString(item.BackupVaultArn)),
		Name:             strings.TrimSpace(aws.ToString(item.BackupVaultName)),
		EncryptionKeyARN: strings.TrimSpace(aws.ToString(item.EncryptionKeyArn)),
		CreationDate:     aws.ToTime(item.CreationDate),
	}
	if item.Locked != nil {
		vault.Locked = *item.Locked
	}
	if item.LockDate != nil {
		vault.LockDate = *item.LockDate
	}
	if item.MinRetentionDays != nil {
		v := *item.MinRetentionDays
		vault.MinRetentionDays = &v
	}
	if item.MaxRetentionDays != nil {
		v := *item.MaxRetentionDays
		vault.MaxRetentionDays = &v
	}
	if item.NumberOfRecoveryPoints != 0 {
		vault.NumberOfRecoveryPoints = item.NumberOfRecoveryPoints
	}
	return vault
}

// mergeDescribedVault folds DescribeBackupVault output into the listed vault
// metadata. It augments encryption key type and lock state; it never copies
// any vault access policy body (DescribeBackupVault does not return one, and
// the adapter never calls GetBackupVaultAccessPolicy).
func mergeDescribedVault(vault *backupservice.Vault, described *awsbackup.DescribeBackupVaultOutput) {
	if vault == nil || described == nil {
		return
	}
	if v := strings.TrimSpace(string(described.EncryptionKeyType)); v != "" {
		vault.EncryptionKeyType = v
	}
	if described.Locked != nil {
		vault.Locked = *described.Locked
	}
	if described.LockDate != nil && !described.LockDate.IsZero() {
		vault.LockDate = *described.LockDate
	}
	if described.MinRetentionDays != nil {
		v := *described.MinRetentionDays
		vault.MinRetentionDays = &v
	}
	if described.MaxRetentionDays != nil {
		v := *described.MaxRetentionDays
		vault.MaxRetentionDays = &v
	}
	if described.NumberOfRecoveryPoints != 0 {
		vault.NumberOfRecoveryPoints = described.NumberOfRecoveryPoints
	}
}

func mapPlanListMember(item awsbackuptypes.BackupPlansListMember) backupservice.Plan {
	return backupservice.Plan{
		ARN:               strings.TrimSpace(aws.ToString(item.BackupPlanArn)),
		ID:                strings.TrimSpace(aws.ToString(item.BackupPlanId)),
		Name:              strings.TrimSpace(aws.ToString(item.BackupPlanName)),
		VersionID:         strings.TrimSpace(aws.ToString(item.VersionId)),
		CreationDate:      aws.ToTime(item.CreationDate),
		LastExecutionDate: aws.ToTime(item.LastExecutionDate),
	}
}

func mapPlanRules(rules []awsbackuptypes.BackupRule) []backupservice.PlanRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]backupservice.PlanRule, 0, len(rules))
	for _, rule := range rules {
		mapped := backupservice.PlanRule{
			Name:                  strings.TrimSpace(aws.ToString(rule.RuleName)),
			ScheduleExpression:    strings.TrimSpace(aws.ToString(rule.ScheduleExpression)),
			TargetBackupVaultName: strings.TrimSpace(aws.ToString(rule.TargetBackupVaultName)),
		}
		if rule.StartWindowMinutes != nil {
			v := *rule.StartWindowMinutes
			mapped.StartWindowMinutes = &v
		}
		if rule.CompletionWindowMinutes != nil {
			v := *rule.CompletionWindowMinutes
			mapped.CompletionWindowMinutes = &v
		}
		if rule.EnableContinuousBackup != nil {
			v := *rule.EnableContinuousBackup
			mapped.EnableContinuousBackup = &v
		}
		out = append(out, mapped)
	}
	return out
}

func mapSelectionListMember(item awsbackuptypes.BackupSelectionsListMember) backupservice.Selection {
	return backupservice.Selection{
		ID:           strings.TrimSpace(aws.ToString(item.SelectionId)),
		Name:         strings.TrimSpace(aws.ToString(item.SelectionName)),
		PlanID:       strings.TrimSpace(aws.ToString(item.BackupPlanId)),
		IAMRoleARN:   strings.TrimSpace(aws.ToString(item.IamRoleArn)),
		CreationDate: aws.ToTime(item.CreationDate),
	}
}

// mergeDescribedSelection folds GetBackupSelection output into the listed
// selection metadata. It captures resources, conditions, and not-resources
// only; resource tag VALUES from matched resources are not part of this map.
func mergeDescribedSelection(selection *backupservice.Selection, body *awsbackup.GetBackupSelectionOutput) {
	if selection == nil || body == nil || body.BackupSelection == nil {
		return
	}
	bs := body.BackupSelection
	if role := strings.TrimSpace(aws.ToString(bs.IamRoleArn)); role != "" {
		selection.IAMRoleARN = role
	}
	if name := strings.TrimSpace(aws.ToString(bs.SelectionName)); name != "" {
		selection.Name = name
	}
	selection.Resources = trimAndCopyStrings(bs.Resources)
	selection.NotResources = trimAndCopyStrings(bs.NotResources)
	selection.TagConditions = mergeTagConditions(bs.ListOfTags, bs.Conditions)
}

func mergeTagConditions(
	listOfTags []awsbackuptypes.Condition,
	conditions *awsbackuptypes.Conditions,
) []backupservice.TagCondition {
	var out []backupservice.TagCondition
	for _, cond := range listOfTags {
		out = append(out, backupservice.TagCondition{
			Operator: conditionTypeOperator(cond.ConditionType),
			Key:      strings.TrimSpace(aws.ToString(cond.ConditionKey)),
			Value:    strings.TrimSpace(aws.ToString(cond.ConditionValue)),
		})
	}
	if conditions != nil {
		out = append(out, mapConditionParameters("StringEquals", conditions.StringEquals)...)
		out = append(out, mapConditionParameters("StringNotEquals", conditions.StringNotEquals)...)
		out = append(out, mapConditionParameters("StringLike", conditions.StringLike)...)
		out = append(out, mapConditionParameters("StringNotLike", conditions.StringNotLike)...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// conditionTypeOperator maps an AWS Backup ListOfTags ConditionType enum to
// the camelCase operator vocabulary the scanner persists, keeping ListOfTags
// operators aligned with the Conditions-block operators in mergeTagConditions.
// AWS reports the type as a required enum; an empty value defaults to
// StringEquals (the only operator ListOfTags historically supported), while an
// unrecognized value is preserved verbatim so a future enum expansion is
// recorded faithfully rather than mislabeled as equality.
func conditionTypeOperator(ct awsbackuptypes.ConditionType) string {
	switch ct {
	case awsbackuptypes.ConditionType("STRINGEQUALS"):
		return "StringEquals"
	case awsbackuptypes.ConditionType("STRINGNOTEQUALS"):
		return "StringNotEquals"
	case awsbackuptypes.ConditionType("STRINGLIKE"):
		return "StringLike"
	case awsbackuptypes.ConditionType("STRINGNOTLIKE"):
		return "StringNotLike"
	case awsbackuptypes.ConditionType(""):
		return "StringEquals"
	default:
		return string(ct)
	}
}

func mapConditionParameters(
	operator string,
	params []awsbackuptypes.ConditionParameter,
) []backupservice.TagCondition {
	if len(params) == 0 {
		return nil
	}
	out := make([]backupservice.TagCondition, 0, len(params))
	for _, p := range params {
		out = append(out, backupservice.TagCondition{
			Operator: operator,
			Key:      strings.TrimSpace(aws.ToString(p.ConditionKey)),
			Value:    strings.TrimSpace(aws.ToString(p.ConditionValue)),
		})
	}
	return out
}

func mapRecoveryPoint(item awsbackuptypes.RecoveryPointByBackupVault) backupservice.RecoveryPoint {
	rp := backupservice.RecoveryPoint{
		ARN:                strings.TrimSpace(aws.ToString(item.RecoveryPointArn)),
		VaultName:          strings.TrimSpace(aws.ToString(item.BackupVaultName)),
		VaultARN:           strings.TrimSpace(aws.ToString(item.BackupVaultArn)),
		SourceResourceARN:  strings.TrimSpace(aws.ToString(item.ResourceArn)),
		SourceResourceType: strings.TrimSpace(aws.ToString(item.ResourceType)),
		Status:             strings.TrimSpace(string(item.Status)),
		StatusMessage:      strings.TrimSpace(aws.ToString(item.StatusMessage)),
		EncryptionKeyARN:   strings.TrimSpace(aws.ToString(item.EncryptionKeyArn)),
		IsEncrypted:        item.IsEncrypted,
		CreationDate:       aws.ToTime(item.CreationDate),
		CompletionDate:     aws.ToTime(item.CompletionDate),
	}
	if item.CalculatedLifecycle != nil {
		rp.CalculatedDeleteAt = aws.ToTime(item.CalculatedLifecycle.DeleteAt)
	}
	if item.BackupSizeInBytes != nil {
		v := *item.BackupSizeInBytes
		rp.BackupSizeInBytes = &v
	}
	return rp
}

func mapReportPlan(item awsbackuptypes.ReportPlan) backupservice.ReportPlan {
	plan := backupservice.ReportPlan{
		ARN:                         strings.TrimSpace(aws.ToString(item.ReportPlanArn)),
		Name:                        strings.TrimSpace(aws.ToString(item.ReportPlanName)),
		DeploymentStatus:            strings.TrimSpace(aws.ToString(item.DeploymentStatus)),
		CreationTime:                aws.ToTime(item.CreationTime),
		LastAttemptedExecutionTime:  aws.ToTime(item.LastAttemptedExecutionTime),
		LastSuccessfulExecutionTime: aws.ToTime(item.LastSuccessfulExecutionTime),
	}
	if item.ReportSetting != nil {
		plan.ReportTemplate = strings.TrimSpace(aws.ToString(item.ReportSetting.ReportTemplate))
	}
	if item.ReportDeliveryChannel != nil {
		plan.S3BucketName = strings.TrimSpace(aws.ToString(item.ReportDeliveryChannel.S3BucketName))
		plan.S3KeyPrefix = strings.TrimSpace(aws.ToString(item.ReportDeliveryChannel.S3KeyPrefix))
		plan.Formats = trimAndCopyStrings(item.ReportDeliveryChannel.Formats)
	}
	return plan
}

func mapRestoreTestingPlan(item awsbackuptypes.RestoreTestingPlanForList) backupservice.RestoreTestingPlan {
	return backupservice.RestoreTestingPlan{
		ARN:                strings.TrimSpace(aws.ToString(item.RestoreTestingPlanArn)),
		Name:               strings.TrimSpace(aws.ToString(item.RestoreTestingPlanName)),
		ScheduleExpression: strings.TrimSpace(aws.ToString(item.ScheduleExpression)),
		ScheduleTimezone:   strings.TrimSpace(aws.ToString(item.ScheduleExpressionTimezone)),
		CreationTime:       aws.ToTime(item.CreationTime),
		LastExecutionTime:  aws.ToTime(item.LastExecutionTime),
		LastUpdateTime:     aws.ToTime(item.LastUpdateTime),
	}
}

func mapFrameworkListItem(item awsbackuptypes.Framework) backupservice.Framework {
	return backupservice.Framework{
		ARN:              strings.TrimSpace(aws.ToString(item.FrameworkArn)),
		Name:             strings.TrimSpace(aws.ToString(item.FrameworkName)),
		Description:      strings.TrimSpace(aws.ToString(item.FrameworkDescription)),
		DeploymentStatus: strings.TrimSpace(aws.ToString(item.DeploymentStatus)),
		NumberOfControls: item.NumberOfControls,
		CreationTime:     aws.ToTime(item.CreationTime),
	}
}

// mapFrameworkControls projects framework controls into the metadata-only
// shape Eshu persists. Control input parameter VALUES are intentionally
// dropped; only the control name and a compact scope summary (compliance
// resource type list and tag-key list) are surfaced.
func mapFrameworkControls(controls []awsbackuptypes.FrameworkControl) []backupservice.FrameworkControl {
	if len(controls) == 0 {
		return nil
	}
	out := make([]backupservice.FrameworkControl, 0, len(controls))
	for _, control := range controls {
		mapped := backupservice.FrameworkControl{
			Name: strings.TrimSpace(aws.ToString(control.ControlName)),
		}
		if scope := control.ControlScope; scope != nil {
			mapped.ScopeComplianceTypes = trimAndCopyStrings(scope.ComplianceResourceTypes)
			mapped.ScopeResourceTypeCount = len(scope.ComplianceResourceIds)
			mapped.ScopeTagKeys = tagKeys(scope.Tags)
		}
		out = append(out, mapped)
	}
	return out
}

func tagKeys(tags map[string]string) []string {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

func trimAndCopyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
