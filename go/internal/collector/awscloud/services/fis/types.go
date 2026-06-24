// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fis

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Fault Injection Service experiment-template
// observations for one AWS claim. Implementations read control-plane metadata
// through the FIS management APIs and never start, stop, or mutate an
// experiment, and never read experiment run results or resolved-target
// inventories.
type Client interface {
	// Snapshot returns every FIS experiment template visible to the configured
	// AWS credentials, each carrying its action, target, logging, and
	// stop-condition metadata.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures FIS experiment-template metadata plus non-fatal scan
// warnings.
type Snapshot struct {
	// Templates is the metadata-only set of FIS experiment templates.
	Templates []ExperimentTemplate
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// ExperimentTemplate is the scanner-owned FIS experiment-template model. It
// carries control-plane metadata only and intentionally excludes action
// parameter values, target filter values, and any experiment run output.
type ExperimentTemplate struct {
	// ID is the FIS experiment template id (EXTxxxxxxxx).
	ID string
	// ARN is the Amazon Resource Name that uniquely identifies the template.
	ARN string
	// Name is the template's Name tag value when present; FIS templates have no
	// dedicated name field.
	Name string
	// Description is the template description.
	Description string
	// RoleARN is the IAM role ARN FIS assumes to inject faults.
	RoleARN string
	// Actions are the action ids and the fault-injection action each runs.
	Actions []Action
	// Targets are the named target selectors the template resolves resources
	// through, plus any explicitly listed resource ARNs.
	Targets []Target
	// LogGroupARN is the CloudWatch Logs log group ARN experiment logs stream to,
	// when CloudWatch logging is configured.
	LogGroupARN string
	// LogS3Bucket is the destination S3 bucket name for experiment logs, when S3
	// logging is configured. It is a bucket name, not an ARN.
	LogS3Bucket string
	// LogS3Prefix is the optional object-key prefix for the S3 log destination.
	LogS3Prefix string
	// StopConditionAlarmARNs are the CloudWatch alarm ARNs the template halts on.
	StopConditionAlarmARNs []string
	// CreationTime is when the template was created.
	CreationTime time.Time
	// LastUpdateTime is when the template was last updated.
	LastUpdateTime time.Time
	// Tags carries the template resource tags.
	Tags map[string]string
}

// Action is the scanner-owned FIS experiment-template action model. It records
// the action id and the AWS fault-injection action it runs; parameter values
// are intentionally excluded.
type Action struct {
	// Key is the action's key within the template's action map.
	Key string
	// ActionID is the FIS action identifier (for example aws:ec2:stop-instances).
	ActionID string
	// Description is the per-template description for the action.
	Description string
}

// Target is the scanner-owned FIS experiment-template target model. It records
// the target key, the FIS resource type selector, the selection mode, and any
// explicitly listed resource ARNs. Target filter values and resource tag
// selectors are intentionally excluded.
type Target struct {
	// Key is the target's key within the template's target map.
	Key string
	// ResourceType is the FIS resource type selector (for example
	// aws:ec2:instance, aws:ecs:cluster, aws:rds:db-instance).
	ResourceType string
	// SelectionMode scopes the identified resources to a count or percentage.
	SelectionMode string
	// ResourceARNs are the explicit resource ARNs the target lists, when the
	// template targets resources by ARN rather than by tag or filter.
	ResourceARNs []string
}
