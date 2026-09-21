// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resiliencehub

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Resilience Hub metadata-only facts for one claimed account
// and region. It reports applications, resiliency policies, application
// components, input sources, and assessments, plus the app-uses-policy,
// app-protects-resource, component-in-app, input-source-in-app, and
// assessment-for-app relationships. It never persists assessment result bodies,
// drift detail, recommendation contents, or any data-plane payload.
type Scanner struct {
	// Client is the metadata-only Resilience Hub snapshot source.
	Client Client
}

// Scan observes Resilience Hub applications, their policies, components, input
// sources, protected physical resources, and assessments through the configured
// client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("resiliencehub scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceResilienceHub:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceResilienceHub
	default:
		return nil, fmt.Errorf("resiliencehub scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Resilience Hub: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, policy := range snapshot.Policies {
		next, err := policyEnvelopes(boundary, policy)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, app := range snapshot.Apps {
		next, err := appEnvelopes(boundary, app)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
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

func policyEnvelopes(boundary awscloud.Boundary, policy ResiliencyPolicy) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(policyObservation(boundary, policy))
	if err != nil {
		return nil, err
	}
	return []facts.Envelope{resource}, nil
}

func appEnvelopes(boundary awscloud.Boundary, app App) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(appObservation(boundary, app))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	if err := appendRelationships(&envelopes, appRelationships(boundary, app)...); err != nil {
		return nil, err
	}

	for _, component := range app.Components {
		next, err := componentEnvelopes(boundary, app, component)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, source := range app.InputSources {
		next, err := inputSourceEnvelopes(boundary, app, source)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, assessment := range app.Assessments {
		next, err := assessmentEnvelopes(boundary, assessment)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

// appRelationships returns the relationships sourced directly on the application
// node: its policy edge and one protects-resource edge per ARN-keyable physical
// resource. Unresolvable physical resources are skipped, not dangled.
func appRelationships(boundary awscloud.Boundary, app App) []*awscloud.RelationshipObservation {
	relationships := []*awscloud.RelationshipObservation{
		appUsesPolicyRelationship(boundary, app),
	}
	for _, resource := range app.ProtectedResources {
		relationships = append(relationships, appProtectsResourceRelationship(boundary, app, resource))
	}
	return relationships
}

func componentEnvelopes(
	boundary awscloud.Boundary,
	app App,
	component AppComponent,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(componentObservation(boundary, app, component))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if err := appendRelationships(&envelopes, componentInAppRelationship(boundary, app, component)); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func inputSourceEnvelopes(
	boundary awscloud.Boundary,
	app App,
	source InputSource,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(inputSourceObservation(boundary, app, source))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if err := appendRelationships(&envelopes, inputSourceInAppRelationship(boundary, app, source)); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func assessmentEnvelopes(boundary awscloud.Boundary, assessment Assessment) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(assessmentObservation(boundary, assessment))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if err := appendRelationships(&envelopes, assessmentForAppRelationship(boundary, assessment)); err != nil {
		return nil, err
	}
	return envelopes, nil
}

// appendRelationships appends an envelope for each non-nil relationship,
// skipping nil entries so callers can pass optional edges inline.
func appendRelationships(
	envelopes *[]facts.Envelope,
	relationships ...*awscloud.RelationshipObservation,
) error {
	for _, relationship := range relationships {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}
