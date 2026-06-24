// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package computeoptimizer

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Compute Optimizer metadata-only facts for one claimed
// account and region. It reports recommendation summaries (one per resource
// type) and per-resource recommendations for EC2 instances, Auto Scaling groups,
// EBS volumes, and Lambda functions, plus the recommendation-to-target edges to
// the analyzed instance, group, and function. It never mutates Compute Optimizer
// state, never enrolls an account, and never persists the CloudWatch utilization
// metric data points behind a recommendation.
type Scanner struct {
	// Client is the metadata-only Compute Optimizer snapshot source.
	Client Client
}

// Scan observes Compute Optimizer recommendation summaries, per-resource
// recommendations, and the recommendation-to-target relationships through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("compute optimizer scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceComputeOptimizer:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceComputeOptimizer
	default:
		return nil, fmt.Errorf("compute optimizer scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Compute Optimizer recommendations: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, summary := range snapshot.Summaries {
		if err := appendResource(&envelopes, summaryObservation(boundary, summary)); err != nil {
			return nil, err
		}
	}
	if err := appendInstanceRecommendations(&envelopes, boundary, snapshot.InstanceRecommendations); err != nil {
		return nil, err
	}
	if err := appendAutoScalingGroupRecommendations(&envelopes, boundary, snapshot.AutoScalingGroupRecommendations); err != nil {
		return nil, err
	}
	if err := appendVolumeRecommendations(&envelopes, boundary, snapshot.VolumeRecommendations); err != nil {
		return nil, err
	}
	if err := appendLambdaFunctionRecommendations(&envelopes, boundary, snapshot.LambdaFunctionRecommendations); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func appendInstanceRecommendations(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	recs []InstanceRecommendation,
) error {
	for _, rec := range recs {
		if err := appendResource(envelopes, instanceObservation(boundary, rec)); err != nil {
			return err
		}
		if err := appendRelationship(envelopes, instanceTargetRelationship(boundary, rec)); err != nil {
			return err
		}
	}
	return nil
}

func appendAutoScalingGroupRecommendations(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	recs []AutoScalingGroupRecommendation,
) error {
	for _, rec := range recs {
		if err := appendResource(envelopes, autoScalingGroupObservation(boundary, rec)); err != nil {
			return err
		}
		if err := appendRelationship(envelopes, autoScalingGroupTargetRelationship(boundary, rec)); err != nil {
			return err
		}
	}
	return nil
}

func appendVolumeRecommendations(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	recs []VolumeRecommendation,
) error {
	// EBS volume recommendations have no graph edge in this scanner yet. The
	// volume identity is recorded as recommendation metadata until a dedicated
	// recommendation-to-volume relationship slice lands.
	for _, rec := range recs {
		if err := appendResource(envelopes, volumeObservation(boundary, rec)); err != nil {
			return err
		}
	}
	return nil
}

func appendLambdaFunctionRecommendations(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	recs []LambdaFunctionRecommendation,
) error {
	for _, rec := range recs {
		if err := appendResource(envelopes, lambdaFunctionObservation(boundary, rec)); err != nil {
			return err
		}
		if err := appendRelationship(envelopes, lambdaFunctionTargetRelationship(boundary, rec)); err != nil {
			return err
		}
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

func appendRelationship(envelopes *[]facts.Envelope, relationship *awscloud.RelationshipObservation) error {
	if relationship == nil {
		return nil
	}
	envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
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
