// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backup

import (
	"context"
	"time"
)

// Client is the AWS Backup metadata read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into scanner-owned metadata records.
//
// The interface is intentionally narrow: it covers only the inventory and
// metadata reads Eshu needs. It must NEVER expose Create/Update/Delete vault,
// plan, selection, report plan, restore testing plan, framework, or recovery
// point operations, nor StartBackupJob/StartRestoreJob/StartCopyJob,
// PutBackupVaultAccessPolicy, GetRecoveryPointRestoreMetadata, or
// GetBackupVaultAccessPolicy. Tests assert the surface by asserting only the
// listed methods are present on the interface.
type Client interface {
	ListBackupVaults(context.Context) ([]Vault, error)
	ListBackupPlans(context.Context) ([]Plan, error)
	ListBackupSelections(ctx context.Context, planID string) ([]Selection, error)
	ListRecoveryPoints(ctx context.Context, vaultName string) ([]RecoveryPoint, error)
	ListReportPlans(context.Context) ([]ReportPlan, error)
	ListRestoreTestingPlans(context.Context) ([]RestoreTestingPlan, error)
	ListFrameworks(context.Context) ([]Framework, error)
}

// Vault is the metadata-only scanner view of one AWS Backup vault.
//
// Vault access policy bodies are intentionally outside the contract. The
// presence of an access policy is captured as a boolean only; the JSON
// document body, statements, and principal lists are never persisted because
// they encode cross-account trust.
type Vault struct {
	ARN                    string
	Name                   string
	EncryptionKeyARN       string
	EncryptionKeyType      string
	Locked                 bool
	LockDate               time.Time
	MinRetentionDays       *int64
	MaxRetentionDays       *int64
	NumberOfRecoveryPoints int64
	CreationDate           time.Time
	HasAccessPolicy        bool
}

// Plan is the metadata-only scanner view of one AWS Backup plan. The plan
// version is the durable identifier AWS reports; plan-body JSON is reduced to
// rule-level identity and schedule/target-vault metadata.
type Plan struct {
	ARN               string
	ID                string
	Name              string
	VersionID         string
	CreationDate      time.Time
	LastExecutionDate time.Time
	Rules             []PlanRule
}

// PlanRule is the safe rule-level projection of a backup plan. It carries the
// rule name, the schedule expression, the target vault name, and the
// completion/start-window minutes only. Lifecycle policy values, copy
// actions, and recovery point tags stay outside the projection.
type PlanRule struct {
	Name                    string
	ScheduleExpression      string
	TargetBackupVaultName   string
	StartWindowMinutes      *int64
	CompletionWindowMinutes *int64
	EnableContinuousBackup  *bool
}

// Selection is the metadata-only scanner view of one AWS Backup selection.
// Conditions reflect tag-filter assignment metadata; the scanner records key
// and operator only and never the matched resource tags themselves.
type Selection struct {
	ID            string
	Name          string
	PlanID        string
	IAMRoleARN    string
	Resources     []string
	NotResources  []string
	TagConditions []TagCondition
	CreationDate  time.Time
}

// TagCondition is the metadata-only scanner view of an AWS Backup selection
// tag-filter condition.
type TagCondition struct {
	Operator string
	Key      string
	Value    string
}

// RecoveryPoint is the metadata-only scanner view of one AWS Backup recovery
// point.
//
// The scanner persists identity and timing metadata only. Snapshot contents
// must NEVER appear in this record, and recovery-point restore metadata
// (which can carry source-resource configuration values) must NEVER be read
// or stored.
type RecoveryPoint struct {
	ARN                string
	VaultName          string
	VaultARN           string
	SourceResourceARN  string
	SourceResourceType string
	Status             string
	StatusMessage      string
	EncryptionKeyARN   string
	IsEncrypted        bool
	CreationDate       time.Time
	CompletionDate     time.Time
	CalculatedDeleteAt time.Time
	BackupSizeInBytes  *int64
	StorageClass       string
}

// ReportPlan is the metadata-only scanner view of one AWS Backup report plan.
// Report template parameter values are not persisted; only the template name
// and S3 destination metadata are captured.
type ReportPlan struct {
	ARN                         string
	Name                        string
	DeploymentStatus            string
	ReportTemplate              string
	Formats                     []string
	S3BucketName                string
	S3KeyPrefix                 string
	CreationTime                time.Time
	LastAttemptedExecutionTime  time.Time
	LastSuccessfulExecutionTime time.Time
}

// RestoreTestingPlan is the metadata-only scanner view of one AWS Backup
// restore testing plan. Selection bodies and recovery-point-selection
// criteria stay outside the contract.
type RestoreTestingPlan struct {
	ARN                string
	Name               string
	ScheduleExpression string
	ScheduleTimezone   string
	CreationTime       time.Time
	LastExecutionTime  time.Time
	LastUpdateTime     time.Time
}

// Framework is the metadata-only scanner view of one AWS Backup framework. The
// scanner persists framework identity, deployment status, and a control
// summary; framework control input parameter VALUES are never persisted.
type Framework struct {
	ARN              string
	Name             string
	Description      string
	DeploymentStatus string
	NumberOfControls int32
	CreationTime     time.Time
	Controls         []FrameworkControl
}

// FrameworkControl is the metadata-only scanner view of one control inside an
// AWS Backup framework. Only the control name and the control scope summary
// are recorded; control input parameter values stay out because they may
// carry compliance-sensitive scope data.
type FrameworkControl struct {
	Name                   string
	ScopeComplianceTypes   []string
	ScopeTagKeys           []string
	ScopeResourceTypeCount int
}
