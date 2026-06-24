// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditmanager

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Audit Manager assessment, framework, and
// control observations for one AWS claim. Implementations read control-plane
// metadata through Audit Manager list/get management APIs and never read
// collected audit evidence, evidence finder records, change logs, delegation
// comments, control narratives, or assessment report URLs.
type Client interface {
	// Snapshot returns every Audit Manager assessment, framework, and control
	// visible to the configured AWS credentials, plus the account-level KMS key
	// Audit Manager uses to encrypt assessment evidence and reports.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Audit Manager metadata plus non-fatal scan warnings.
type Snapshot struct {
	// Assessments is the metadata-only set of Audit Manager assessments.
	Assessments []Assessment
	// Frameworks is the metadata-only set of Audit Manager assessment
	// frameworks (Standard and Custom).
	Frameworks []Framework
	// Controls is the metadata-only set of Audit Manager controls (Standard,
	// Custom, and Core).
	Controls []Control
	// KMSKeyARN is the account-level customer managed KMS key Audit Manager uses
	// to encrypt assessment evidence and reports, reported by GetSettings. It is
	// empty when Audit Manager uses an AWS-owned key or settings are unreadable.
	KMSKeyARN string
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling or an unregistered Audit Manager account.
	Warnings []awscloud.WarningObservation
}

// Assessment is the scanner-owned Audit Manager assessment model. It carries
// control-plane metadata only and intentionally excludes collected evidence,
// evidence folders, delegation comments, and the assessment description text.
type Assessment struct {
	// ARN is the Amazon Resource Name that uniquely identifies the assessment.
	ARN string
	// ID is the Audit Manager assessment id.
	ID string
	// Name is the assessment name.
	Name string
	// ComplianceType is the compliance standard the assessment reports against
	// (for example PCI-DSS), when set.
	ComplianceType string
	// Status is the assessment lifecycle status (for example ACTIVE).
	Status string
	// FrameworkARN is the ARN of the framework the assessment was created from,
	// used to join the framework node.
	FrameworkARN string
	// FrameworkID is the id of the framework the assessment was created from.
	FrameworkID string
	// ReportsS3Destination is the assessment-reports S3 destination URI Audit
	// Manager reports (for example s3://bucket/prefix), when configured. It is a
	// location reference, not report content.
	ReportsS3Destination string
	// ReportsDestinationType is the destination type Audit Manager reports (for
	// example S3), when configured.
	ReportsDestinationType string
	// ScopeAccountIDs are the AWS account ids included in the assessment scope.
	ScopeAccountIDs []string
	// ScopeServiceNames are the AWS service names in the assessment scope. AWS
	// deprecated caller-specified services scope, so this is usually empty; it is
	// recorded as context only, never as an edge.
	ScopeServiceNames []string
	// CreationTime is when the assessment was created.
	CreationTime time.Time
	// LastUpdatedTime is when the assessment was last updated.
	LastUpdatedTime time.Time
	// Tags carries the assessment resource tags.
	Tags map[string]string
}

// Framework is the scanner-owned Audit Manager assessment framework model. It
// carries control-plane metadata only and intentionally excludes the framework
// description text and control narrative bodies.
type Framework struct {
	// ARN is the Amazon Resource Name that uniquely identifies the framework.
	ARN string
	// ID is the Audit Manager framework id.
	ID string
	// Name is the framework name.
	Name string
	// ComplianceType is the compliance standard the framework supports (for
	// example PCI DSS or HIPAA), when set.
	ComplianceType string
	// Type is the framework type (Standard or Custom).
	Type string
	// ControlSetsCount is the number of control sets associated with the
	// framework.
	ControlSetsCount int32
	// ControlsCount is the number of controls associated with the framework.
	ControlsCount int32
	// CreatedAt is when the framework was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the framework was last updated.
	LastUpdatedAt time.Time
}

// Control is the scanner-owned Audit Manager control model. It carries
// control-plane metadata only and intentionally excludes control
// testing-information, action-plan instructions, and control-mapping source
// rule bodies.
type Control struct {
	// ARN is the Amazon Resource Name that uniquely identifies the control.
	ARN string
	// ID is the Audit Manager control id.
	ID string
	// Name is the control name.
	Name string
	// Type is the control type (Standard, Custom, or Core).
	Type string
	// ControlSources is the evidence data-source category names Audit Manager
	// collects evidence from for the control (for example "AWS Config,
	// AWS Security Hub"). It is a category label, never evidence content.
	ControlSources string
	// CreatedAt is when the control was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the control was last updated.
	LastUpdatedAt time.Time
}
