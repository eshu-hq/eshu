// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// S3ExternalPrincipalGrant is the schema-version-1 typed payload for
// "s3_external_principal_grant".
type S3ExternalPrincipalGrant struct {
	AccountID           string   `json:"account_id"`
	Region              string   `json:"region"`
	ServiceKind         *string  `json:"service_kind,omitempty"`
	CollectorInstanceID *string  `json:"collector_instance_id,omitempty"`
	BucketARN           *string  `json:"bucket_arn,omitempty"`
	BucketName          *string  `json:"bucket_name,omitempty"`
	PrincipalKind       string   `json:"principal_kind"`
	PrincipalValue      string   `json:"principal_value"`
	PrincipalAccountID  *string  `json:"principal_account_id,omitempty"`
	PrincipalPartition  *string  `json:"principal_partition,omitempty"`
	PrincipalService    *string  `json:"principal_service,omitempty"`
	GrantOutcome        string   `json:"grant_outcome"`
	IsPublic            bool     `json:"is_public"`
	IsCrossAccount      bool     `json:"is_cross_account"`
	IsServicePrincipal  bool     `json:"is_service_principal"`
	IsUnsupported       bool     `json:"is_unsupported"`
	UnsupportedKey      *string  `json:"unsupported_key,omitempty"`
	SourceStatementID   *string  `json:"source_statement_id,omitempty"`
	ResolutionMode      *string  `json:"resolution_mode,omitempty"`
	CorrelationAnchors  []string `json:"correlation_anchors,omitempty"`
}
