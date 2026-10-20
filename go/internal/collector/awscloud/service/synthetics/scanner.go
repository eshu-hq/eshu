// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package synthetics

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon CloudWatch Synthetics metadata-only facts for one claimed
// account and region. It never reads canary script source code, run artifacts
// (logs, screenshots, HAR files), or run results, and never mutates Synthetics
// state. It reports canaries plus the canary-to-S3-artifact-bucket,
// canary-to-IAM-execution-role, and (for VPC-configured canaries)
// canary-to-subnet and canary-to-security-group relationships.
type Scanner struct {
	// Client is the metadata-only Synthetics snapshot source.
	Client Client
}

// Scan observes Synthetics canaries and their reported S3, IAM role, and VPC
// dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("synthetics scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceSynthetics:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceSynthetics
	default:
		return nil, fmt.Errorf("synthetics scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Synthetics canaries: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, canary := range snapshot.Canaries {
		if err := appendResource(&envelopes, canaryObservation(boundary, canary)); err != nil {
			return nil, err
		}
		if err := appendRelationships(&envelopes, canaryRelationships(boundary, canary)); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func appendResource(envelopes *[]facts.Envelope, observation awscloud.ResourceObservation) error {
	envelope, err := awscloud.NewResourceEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func appendRelationships(envelopes *[]facts.Envelope, observations []awscloud.RelationshipObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

// canaryObservation maps a canary into a reported-confidence resource fact. It
// records only control-plane metadata: identity, runtime version, status,
// schedule, retention, run resource limits, artifact encryption mode, and
// timeline. Canary script source code and run artifacts are never recorded.
func canaryObservation(boundary awscloud.Boundary, canary Canary) awscloud.ResourceObservation {
	arn := strings.TrimSpace(canary.ARN)
	name := strings.TrimSpace(canary.Name)
	resourceID := canaryResourceID(canary)
	attributes := map[string]any{
		"canary_id":                        strings.TrimSpace(canary.ID),
		"runtime_version":                  strings.TrimSpace(canary.RuntimeVersion),
		"engine_arn":                       strings.TrimSpace(canary.EngineARN),
		"state_reason_code":                strings.TrimSpace(canary.StateReasonCode),
		"schedule_expression":              strings.TrimSpace(canary.ScheduleExpression),
		"schedule_duration_in_seconds":     canary.ScheduleDurationInSeconds,
		"success_retention_period_in_days": canary.SuccessRetentionPeriodInDays,
		"failure_retention_period_in_days": canary.FailureRetentionPeriodInDays,
		"run_timeout_in_seconds":           canary.RunTimeoutInSeconds,
		"run_memory_in_mb":                 canary.RunMemoryInMB,
		"run_active_tracing":               canary.RunActiveTracing,
		"artifact_encryption_mode":         strings.TrimSpace(canary.ArtifactEncryptionMode),
		"vpc_id":                           strings.TrimSpace(canary.VPCID),
		"vpc_configured":                   len(cloneStrings(canary.SubnetIDs)) > 0 || len(cloneStrings(canary.SecurityGroupIDs)) > 0,
		"created":                          timeOrNil(canary.Created),
		"last_modified":                    timeOrNil(canary.LastModified),
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeSyntheticsCanary,
		Name:               name,
		State:              strings.TrimSpace(canary.State),
		Tags:               cloneStringMap(canary.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
