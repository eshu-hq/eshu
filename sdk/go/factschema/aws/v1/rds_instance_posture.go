// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// RDSInstancePosture is the schema-version-1 typed payload for
// "rds_instance_posture".
type RDSInstancePosture struct {
	AccountID                        string             `json:"account_id"`
	Region                           string             `json:"region"`
	ServiceKind                      *string            `json:"service_kind,omitempty"`
	CollectorInstanceID              *string            `json:"collector_instance_id,omitempty"`
	ARN                              *string            `json:"arn,omitempty"`
	ResourceID                       *string            `json:"resource_id,omitempty"`
	ResourceType                     *string            `json:"resource_type,omitempty"`
	Identifier                       *string            `json:"identifier,omitempty"`
	Engine                           *string            `json:"engine,omitempty"`
	PubliclyAccessible               bool               `json:"publicly_accessible"`
	StorageEncrypted                 bool               `json:"storage_encrypted"`
	KMSKeyID                         *string            `json:"kms_key_id,omitempty"`
	IAMDatabaseAuthenticationEnabled bool               `json:"iam_database_authentication_enabled"`
	MultiAZ                          bool               `json:"multi_az"`
	DeletionProtection               bool               `json:"deletion_protection"`
	BackupRetentionPeriod            int32              `json:"backup_retention_period"`
	PerformanceInsightsEnabled       bool               `json:"performance_insights_enabled"`
	PerformanceInsightsRetentionDays int32              `json:"performance_insights_retention_days"`
	PerformanceInsightsKMSKeyID      *string            `json:"performance_insights_kms_key_id,omitempty"`
	CACertificateIdentifier          *string            `json:"ca_certificate_identifier,omitempty"`
	ParameterGroups                  []string           `json:"parameter_groups,omitempty"`
	OptionGroups                     []string           `json:"option_groups,omitempty"`
	SecurityParameters               *map[string]string `json:"security_parameters,omitempty"`
	CorrelationAnchors               []string           `json:"correlation_anchors,omitempty"`
}
