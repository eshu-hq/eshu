// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package proton

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Proton control-plane metadata-only facts for one claimed
// account and region. It reports environments, services, environment templates,
// and service templates, plus the service-in-environment (derived from service
// instances) and environment-uses-IAM-role relationships. It never reads or
// persists service/environment spec manifest bodies, template schema bodies, or
// deployment input parameter values, and never mutates Proton state.
type Scanner struct {
	// Client is the metadata-only Proton snapshot source.
	Client Client
}

// Scan observes Proton environments, services, templates, and the service
// placement and IAM-role dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("proton scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceProton:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceProton
	default:
		return nil, fmt.Errorf("proton scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Proton control plane: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}

	environmentARNByName := make(map[string]string, len(snapshot.Environments))
	for _, environment := range snapshot.Environments {
		next, err := environmentEnvelopes(boundary, environment)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
		if name := strings.TrimSpace(environment.Name); name != "" {
			environmentARNByName[name] = environmentResourceID(environment)
		}
	}

	for _, service := range snapshot.Services {
		resource, err := awscloud.NewResourceEnvelope(serviceObservation(boundary, service))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	for _, template := range snapshot.EnvironmentTemplates {
		resource, err := awscloud.NewResourceEnvelope(templateObservation(boundary, template, awscloud.ResourceTypeProtonEnvironmentTemplate))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	for _, template := range snapshot.ServiceTemplates {
		resource, err := awscloud.NewResourceEnvelope(templateObservation(boundary, template, awscloud.ResourceTypeProtonServiceTemplate))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	placementEdges, err := placementRelationshipEnvelopes(boundary, snapshot, environmentARNByName)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, placementEdges...)

	return envelopes, nil
}

// environmentEnvelopes builds the environment resource envelope plus the
// environment-uses-IAM-role edge when a Proton service role is reported.
func environmentEnvelopes(boundary awscloud.Boundary, environment Environment) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(environmentObservation(boundary, environment))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := environmentRoleRelationship(boundary, environment); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// placementRelationshipEnvelopes builds the service-in-environment edges from
// the snapshot's service instances. It keys each edge by the environment ARN the
// environment node publishes, resolved from environmentARNByName, and dedupes
// repeated (service, environment) pairs so multiple instances of one service in
// one environment yield a single edge. A placement that names an environment the
// scanner did not observe is skipped rather than dangled.
func placementRelationshipEnvelopes(
	boundary awscloud.Boundary,
	snapshot Snapshot,
	environmentARNByName map[string]string,
) ([]facts.Envelope, error) {
	serviceARNByName := make(map[string]string, len(snapshot.Services))
	for _, service := range snapshot.Services {
		if name := strings.TrimSpace(service.Name); name != "" {
			serviceARNByName[name] = strings.TrimSpace(service.ARN)
		}
	}

	seen := make(map[string]struct{}, len(snapshot.ServicePlacements))
	var envelopes []facts.Envelope
	for _, placement := range snapshot.ServicePlacements {
		serviceName := strings.TrimSpace(placement.ServiceName)
		environmentName := strings.TrimSpace(placement.EnvironmentName)
		if serviceName == "" || environmentName == "" {
			continue
		}
		environmentID := strings.TrimSpace(environmentARNByName[environmentName])
		if environmentID == "" {
			// The placement references an environment the scanner did not
			// observe (for example a cross-account environment); skip rather
			// than key an edge to an environment node that will never exist.
			continue
		}
		serviceID := firstNonEmpty(serviceARNByName[serviceName], serviceName)
		dedupeKey := serviceID + "\x00" + environmentID
		if _, ok := seen[dedupeKey]; ok {
			continue
		}
		seen[dedupeKey] = struct{}{}
		relationship := serviceInEnvironmentRelationship(boundary, serviceID, serviceARNByName[serviceName], environmentID)
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
