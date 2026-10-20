// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package controltower

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Control Tower metadata-only facts for one claimed account
// and region. It reports the landing zone, enabled controls, and enabled
// baselines plus the control-governs-target, baseline-governs-target, and
// baseline-for-landing-zone relationships. It never reads or persists the
// landing-zone manifest body, control or baseline parameter values, or any
// governance payload, and never mutates Control Tower state.
type Scanner struct {
	// Client is the metadata-only Control Tower snapshot source.
	Client Client
}

// Scan observes the Control Tower landing zone, enabled controls, and enabled
// baselines plus their Organizations and landing-zone relationships through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("controltower scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceControlTower:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceControlTower
	default:
		return nil, fmt.Errorf("controltower scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot AWS Control Tower metadata: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, boundary, snapshot.Warnings); err != nil {
		return nil, err
	}

	landingZoneARN := ""
	if snapshot.LandingZone != nil {
		landingZoneARN = strings.TrimSpace(snapshot.LandingZone.ARN)
		resource, err := awscloud.NewResourceEnvelope(landingZoneObservation(boundary, *snapshot.LandingZone))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	for _, control := range snapshot.EnabledControls {
		next, err := controlEnvelopes(boundary, control)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	for _, baseline := range snapshot.EnabledBaselines {
		next, err := baselineEnvelopes(boundary, baseline, landingZoneARN)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	return envelopes, nil
}

func appendWarnings(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	warnings []awscloud.WarningObservation,
) error {
	for _, warning := range warnings {
		warning.Boundary = boundary
		envelope, err := awscloud.NewWarningEnvelope(warning)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func controlEnvelopes(boundary awscloud.Boundary, control EnabledControl) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(controlObservation(boundary, control))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := controlGovernsTargetRelationship(boundary, control); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func baselineEnvelopes(
	boundary awscloud.Boundary,
	baseline EnabledBaseline,
	landingZoneARN string,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(baselineObservation(boundary, baseline))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		baselineGovernsTargetRelationship(boundary, baseline),
		baselineForLandingZoneRelationship(boundary, baseline, landingZoneARN),
	} {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func landingZoneObservation(boundary awscloud.Boundary, landingZone LandingZone) awscloud.ResourceObservation {
	arn := strings.TrimSpace(landingZone.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeControlTowerLandingZone,
		State:        strings.TrimSpace(landingZone.Status),
		Tags:         cloneStringMap(landingZone.Tags),
		Attributes: map[string]any{
			"version":                  strings.TrimSpace(landingZone.Version),
			"latest_available_version": strings.TrimSpace(landingZone.LatestAvailableVersion),
			"drift_status":             strings.TrimSpace(landingZone.DriftStatus),
		},
		CorrelationAnchors: []string{arn},
		SourceRecordID:     arn,
	}
}

func controlObservation(boundary awscloud.Boundary, control EnabledControl) awscloud.ResourceObservation {
	arn := strings.TrimSpace(control.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeControlTowerEnabledControl,
		State:        strings.TrimSpace(control.Status),
		Attributes: map[string]any{
			"control_identifier": strings.TrimSpace(control.ControlIdentifier),
			"target_identifier":  strings.TrimSpace(control.TargetIdentifier),
			"parent_identifier":  strings.TrimSpace(control.ParentIdentifier),
			"drift_status":       strings.TrimSpace(control.DriftStatus),
		},
		CorrelationAnchors: []string{arn, strings.TrimSpace(control.ControlIdentifier)},
		SourceRecordID:     arn,
	}
}

func baselineObservation(boundary awscloud.Boundary, baseline EnabledBaseline) awscloud.ResourceObservation {
	arn := strings.TrimSpace(baseline.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeControlTowerEnabledBaseline,
		State:        strings.TrimSpace(baseline.Status),
		Attributes: map[string]any{
			"baseline_identifier": strings.TrimSpace(baseline.BaselineIdentifier),
			"baseline_version":    strings.TrimSpace(baseline.BaselineVersion),
			"target_identifier":   strings.TrimSpace(baseline.TargetIdentifier),
			"parent_identifier":   strings.TrimSpace(baseline.ParentIdentifier),
			"drift_status":        strings.TrimSpace(baseline.DriftStatus),
		},
		CorrelationAnchors: []string{arn, strings.TrimSpace(baseline.BaselineIdentifier)},
		SourceRecordID:     arn,
	}
}
