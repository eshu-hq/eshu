// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codepipeline

import (
	"context"
	"time"
)

// Client is the metadata-only CodePipeline read surface consumed by Scanner.
// Runtime adapters translate AWS SDK responses into these scanner-owned
// records. The contract lists no mutation, execution-control,
// webhook-management, custom-action-mutation, or job-worker call so the scanner
// cannot reach action configuration secret values or trigger pipeline runs.
type Client interface {
	// ListPipelines returns pipeline metadata for the boundary, including each
	// pipeline's stages and actions. Action configuration values are dropped by
	// the adapter; only configuration key names and allowlisted non-secret
	// target identifiers reach these records.
	ListPipelines(context.Context) ([]Pipeline, error)
	// ListRecentExecutions returns recent execution-summary metadata for one
	// pipeline, bounded to a recent window by the adapter. Source-revision
	// commit-message summaries are redacted before they reach these records.
	ListRecentExecutions(context.Context, string) ([]Execution, error)
	// ListWebhooks returns webhook metadata for the boundary. The webhook
	// authentication secret token is never present in these records.
	ListWebhooks(context.Context) ([]Webhook, error)
	// ListCustomActionTypes returns custom action-type metadata for the
	// boundary. AWS-owned and ThirdParty action types are excluded by the
	// adapter so the scan stays scoped to customer-defined types.
	ListCustomActionTypes(context.Context) ([]ActionType, error)
}

// Pipeline is the scanner-owned representation of one CodePipeline pipeline. It
// carries identity, the artifact-store summary, and the stage/action structure.
// No field can hold an action configuration value.
type Pipeline struct {
	Name          string
	ARN           string
	RoleARN       string
	PipelineType  string
	ExecutionMode string
	Version       int32
	Created       time.Time
	Updated       time.Time
	ArtifactStore ArtifactStoreSummary
	Stages        []Stage
	Tags          map[string]string
}

// ArtifactStoreSummary captures the pipeline artifact-store contract: store
// type, the backing S3 bucket name, and the optional KMS encryption key id. The
// key id may be a key id, key ARN, or alias ARN per the AWS contract.
type ArtifactStoreSummary struct {
	Type       string
	S3Bucket   string
	KMSKeyID   string
	KMSKeyType string
	// Regions lists the cross-region artifact-store region keys when a pipeline
	// uses per-region artifact stores. It carries region identifiers only.
	Regions []string
}

// Stage is the scanner-owned representation of one pipeline stage.
type Stage struct {
	Name    string
	Actions []Action
}

// Action is the scanner-owned representation of one pipeline action. It carries
// the action type identity, the sorted configuration KEY names (never values),
// and an allowlisted non-secret target identifier when the action references a
// concrete build/deploy/invoke target. ConfigurationKeys never includes any
// configuration value.
type Action struct {
	Name              string
	Category          string
	Owner             string
	Provider          string
	Version           string
	RunOrder          int32
	Region            string
	RoleARN           string
	ConfigurationKeys []string
	// TargetProvider names the resolved target provider class for a
	// build/deploy/invoke action (CodeBuild, CodeDeploy, Lambda, CloudFormation,
	// ECS). It is empty for source, approval, and unrecognized providers.
	TargetProvider string
	// TargetResourceName is the allowlisted non-secret identifier the adapter
	// read from a known target-identifier configuration key (for example a
	// CodeBuild ProjectName or a Lambda FunctionName). It is never a secret
	// configuration value. For ECS it is "cluster/service".
	TargetResourceName string
	// SourceProvider names the source provider class for a source action (S3,
	// CodeCommit, GitHub, CodeStarSourceConnection, Bitbucket). It is empty for
	// non-source actions.
	SourceProvider string
}

// Execution is the scanner-owned representation of one recent pipeline
// execution. It carries identity, status, and safe source-revision references.
// Revision diffs are never present.
type Execution struct {
	PipelineName    string
	ID              string
	Status          string
	ExecutionMode   string
	ExecutionType   string
	StartTime       time.Time
	LastUpdateTime  time.Time
	TriggerType     string
	SourceRevisions []SourceRevision
}

// SourceRevision captures non-secret source-revision references for an
// execution. RevisionSummary (the commit message or user-provided summary) is
// redacted by the adapter into SummaryMarker because it may carry a pasted
// secret; the raw summary text never reaches this record. RevisionURL is the
// commit detail page link, not a diff.
type SourceRevision struct {
	ActionName    string
	RevisionID    string
	RevisionURL   string
	HasSummary    bool
	SummaryMarker map[string]any
}

// Webhook is the scanner-owned representation of one CodePipeline webhook. The
// authentication secret token is never present; only the authentication type,
// target pipeline, and target action are retained.
type Webhook struct {
	Name               string
	ARN                string
	TargetPipeline     string
	TargetAction       string
	AuthenticationType string
	LastTriggered      time.Time
	Tags               map[string]string
}

// ActionType is the scanner-owned representation of one custom action type. It
// carries the type identity only; action configuration property values are not
// part of an action type definition.
type ActionType struct {
	Category string
	Owner    string
	Provider string
	Version  string
	// ConfigurationPropertyKeys lists the declared configuration property names
	// for the custom action type. It carries names only.
	ConfigurationPropertyKeys []string
}
