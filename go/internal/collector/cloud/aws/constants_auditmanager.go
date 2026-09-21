// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAuditManager identifies the regional AWS Audit Manager metadata-only
	// scan slice. The scanner reads assessment, framework, and control
	// control-plane metadata through Audit Manager list/get management APIs
	// (ListAssessments, GetAssessment, ListAssessmentFrameworks, ListControls,
	// GetSettings, ListTagsForResource) and never reads collected audit evidence,
	// evidence finder records, change logs, delegation comments, control
	// narratives, or assessment report URLs, and never mutates Audit Manager
	// state.
	ServiceAuditManager = "auditmanager"
)

const (
	// ResourceTypeAuditManagerAssessment identifies an AWS Audit Manager
	// assessment metadata resource. The scanner emits identity, compliance
	// standard, status, framework reference, in-scope account ids/service names,
	// the assessment-reports S3 destination location, and lifecycle timestamps
	// only. Collected evidence, evidence folders, delegation comments, and the
	// assessment description free-text are never persisted.
	ResourceTypeAuditManagerAssessment = "aws_auditmanager_assessment"
	// ResourceTypeAuditManagerFramework identifies an AWS Audit Manager
	// assessment framework metadata resource. The scanner emits identity,
	// compliance standard, framework type (Standard/Custom), control-set and
	// control counts, and lifecycle timestamps only. The framework description
	// free-text and control narrative bodies are never persisted.
	ResourceTypeAuditManagerFramework = "aws_auditmanager_framework"
	// ResourceTypeAuditManagerControl identifies an AWS Audit Manager control
	// metadata resource. The scanner emits identity, control type
	// (Standard/Custom/Core), and evidence data-source category names only.
	// Control testing-information, action-plan instructions, control-mapping
	// source bodies, and Suricata/keyword evidence rules are never persisted.
	ResourceTypeAuditManagerControl = "aws_auditmanager_control"
)

const (
	// RelationshipAuditManagerAssessmentUsesFramework records an assessment's
	// dependency on the Audit Manager framework it was created from. The target
	// is keyed by the framework ARN so the edge joins the framework node the
	// scanner publishes.
	RelationshipAuditManagerAssessmentUsesFramework = "auditmanager_assessment_uses_framework"
	// RelationshipAuditManagerAssessmentReportsToS3 records an assessment's
	// configured assessment-reports S3 destination bucket. Audit Manager reports
	// an s3:// destination URI, so the scanner synthesizes the partition-aware
	// bucket ARN to match the S3 scanner's published bucket resource_id.
	RelationshipAuditManagerAssessmentReportsToS3 = "auditmanager_assessment_reports_to_s3"
	// RelationshipAuditManagerAssessmentEncryptedWithKMSKey records the KMS key
	// Audit Manager uses to encrypt the assessment's evidence and reports. Audit
	// Manager configures one account-level customer managed key (GetSettings), so
	// the edge keys the reported key ARN, matching how the KMS scanner publishes
	// its key resource_id.
	RelationshipAuditManagerAssessmentEncryptedWithKMSKey = "auditmanager_assessment_encrypted_with_kms_key"
	// RelationshipAuditManagerAssessmentInAccount records an AWS account included
	// in the assessment scope. The target keys the partition-aware account root
	// ARN (arn:<partition>:iam::<account-id>:root) so the edge joins the
	// aws_account identity the config, access-analyzer, and ds scanners also
	// target. In-scope service names are deprecated by AWS (the API returns them
	// empty) and are recorded as an assessment attribute, never an edge.
	RelationshipAuditManagerAssessmentInAccount = "auditmanager_assessment_in_account"
)
