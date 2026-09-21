// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appconfig

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS AppConfig metadata-only facts for one claimed account and
// region. It never reads configuration content, hosted configuration version
// bodies, or freeform/feature-flag values, and never mutates AppConfig state or
// starts deployments. It reports applications, environments, configuration
// profiles, and deployment strategies plus the environment/profile-in-
// application, environment-to-CloudWatch-alarm (deployment monitor), and
// environment-to-IAM-role (monitor alarm role) relationships.
type Scanner struct {
	// Client is the metadata-only AppConfig snapshot source.
	Client Client
}

// Scan observes AppConfig applications, their environments and configuration
// profiles, the account-level deployment strategies, and the direct CloudWatch
// alarm and IAM role monitor dependencies through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("appconfig scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceAppConfig:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceAppConfig
	default:
		return nil, fmt.Errorf("appconfig scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AppConfig applications: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, application := range snapshot.Applications {
		next, err := applicationEnvelopes(boundary, application)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, strategy := range snapshot.DeploymentStrategies {
		resource, err := aws.NewResourceEnvelope(deploymentStrategyObservation(boundary, strategy))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []aws.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := aws.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func applicationEnvelopes(boundary aws.Boundary, application Application) ([]facts.Envelope, error) {
	appARN := applicationARN(boundary, application.ID)
	resource, err := aws.NewResourceEnvelope(applicationObservation(boundary, application, appARN))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, environment := range application.Environments {
		next, err := environmentEnvelopes(boundary, appARN, environment)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, profile := range application.Profiles {
		next, err := profileEnvelopes(boundary, appARN, profile)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func environmentEnvelopes(
	boundary aws.Boundary,
	appARN string,
	environment Environment,
) ([]facts.Envelope, error) {
	envARN := environmentARN(boundary, environment.ApplicationID, environment.ID)
	resource, err := aws.NewResourceEnvelope(environmentObservation(boundary, environment, envARN))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	relationships := []*aws.RelationshipObservation{
		environmentInApplicationRelationship(boundary, envARN, appARN),
	}
	for _, monitor := range environment.Monitors {
		relationships = append(
			relationships,
			environmentMonitorsAlarmRelationship(boundary, envARN, monitor),
			environmentMonitorRoleRelationship(boundary, envARN, monitor),
		)
	}
	appended, err := appendRelationships(envelopes, relationships)
	if err != nil {
		return nil, err
	}
	return appended, nil
}

func profileEnvelopes(
	boundary aws.Boundary,
	appARN string,
	profile ConfigurationProfile,
) ([]facts.Envelope, error) {
	profARN := profileARN(boundary, profile.ApplicationID, profile.ID)
	resource, err := aws.NewResourceEnvelope(profileObservation(boundary, profile, profARN))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	return appendRelationships(envelopes, []*aws.RelationshipObservation{
		profileInApplicationRelationship(boundary, profARN, appARN),
	})
}

func appendRelationships(
	envelopes []facts.Envelope,
	relationships []*aws.RelationshipObservation,
) ([]facts.Envelope, error) {
	for _, relationship := range relationships {
		if relationship == nil {
			continue
		}
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func applicationObservation(
	boundary aws.Boundary,
	application Application,
	appARN string,
) aws.ResourceObservation {
	id := strings.TrimSpace(application.ID)
	name := strings.TrimSpace(application.Name)
	resourceID := firstNonEmptyID(appARN, id, name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          appARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAppConfigApplication,
		Name:         name,
		Tags:         cloneStringMap(application.Tags),
		Attributes: map[string]any{
			"application_id": id,
			"description":    strings.TrimSpace(application.Description),
		},
		CorrelationAnchors: []string{appARN, name},
		SourceRecordID:     resourceID,
	}
}

func environmentObservation(
	boundary aws.Boundary,
	environment Environment,
	envARN string,
) aws.ResourceObservation {
	id := strings.TrimSpace(environment.ID)
	name := strings.TrimSpace(environment.Name)
	resourceID := firstNonEmptyID(envARN, id, name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          envARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAppConfigEnvironment,
		Name:         name,
		State:        strings.TrimSpace(environment.State),
		Tags:         cloneStringMap(environment.Tags),
		Attributes: map[string]any{
			"environment_id": id,
			"application_id": strings.TrimSpace(environment.ApplicationID),
			"description":    strings.TrimSpace(environment.Description),
			"monitor_count":  int64(len(environment.Monitors)),
		},
		CorrelationAnchors: []string{envARN, name},
		SourceRecordID:     resourceID,
	}
}

func profileObservation(
	boundary aws.Boundary,
	profile ConfigurationProfile,
	profARN string,
) aws.ResourceObservation {
	id := strings.TrimSpace(profile.ID)
	name := strings.TrimSpace(profile.Name)
	resourceID := firstNonEmptyID(profARN, id, name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          profARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAppConfigConfigurationProfile,
		Name:         name,
		Tags:         cloneStringMap(profile.Tags),
		Attributes: map[string]any{
			"configuration_profile_id": id,
			"application_id":           strings.TrimSpace(profile.ApplicationID),
			"profile_type":             strings.TrimSpace(profile.Type),
			"location_uri":             strings.TrimSpace(profile.LocationURI),
			"validator_types":          cloneStrings(profile.ValidatorTypes),
		},
		CorrelationAnchors: []string{profARN, name},
		SourceRecordID:     resourceID,
	}
}

func deploymentStrategyObservation(
	boundary aws.Boundary,
	strategy DeploymentStrategy,
) aws.ResourceObservation {
	id := strings.TrimSpace(strategy.ID)
	name := strings.TrimSpace(strategy.Name)
	strategyARN := deploymentStrategyARN(boundary, id)
	resourceID := firstNonEmptyID(strategyARN, id, name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          strategyARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAppConfigDeploymentStrategy,
		Name:         name,
		Tags:         cloneStringMap(strategy.Tags),
		Attributes: map[string]any{
			"deployment_strategy_id":         id,
			"description":                    strings.TrimSpace(strategy.Description),
			"deployment_duration_in_minutes": int64(strategy.DeploymentDurationInMinutes),
			"final_bake_time_in_minutes":     int64(strategy.FinalBakeTimeInMinutes),
			"growth_factor":                  float64(strategy.GrowthFactor),
			"growth_type":                    strings.TrimSpace(strategy.GrowthType),
			"replicate_to":                   strings.TrimSpace(strategy.ReplicateTo),
		},
		CorrelationAnchors: []string{strategyARN, name},
		SourceRecordID:     resourceID,
	}
}

// firstNonEmptyID returns the first trimmed non-empty value, or "" when none.
// It keys the resource_id on the synthesized ARN, falling back to id then name,
// so each node publishes a stable identity its own edges can be sourced on.
func firstNonEmptyID(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
