// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sagemaker

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS SageMaker control-plane metadata facts for one claimed
// account and region. It never invokes endpoints, never runs inference, never
// mutates SageMaker state, and never persists hyperparameter values, training
// or processing input/output data contents, notebook lifecycle-config script
// bodies, container environment maps, or pipeline definition bodies.
type Scanner struct {
	Client Client
}

// Scan observes the in-scope SageMaker resource types through the configured
// client and returns aws_resource and aws_relationship fact envelopes. It
// returns an error when the client is missing or the boundary names a
// non-SageMaker service kind.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("sagemaker scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceSageMaker:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceSageMaker
	default:
		return nil, fmt.Errorf("sagemaker scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope
	for _, stage := range s.stages(ctx) {
		observations, relationships, err := stage()
		if err != nil {
			return nil, err
		}
		for _, observation := range observations {
			observation.Boundary = boundary
			envelope, err := awscloud.NewResourceEnvelope(observation)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
		for _, relationship := range relationships {
			relationship.Boundary = boundary
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

// stageFunc collects one resource group's resource and relationship
// observations before they are stamped with the boundary and validated.
type stageFunc func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error)

// stages lists the per-resource-group collection closures in a deterministic
// order so emitted facts and API fanout stay stable across runs.
func (s Scanner) stages(ctx context.Context) []stageFunc {
	return []stageFunc{
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListNotebookInstances(ctx)
			return collect(items, notebookObservation, notebookRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListModels(ctx)
			return collect(items, modelObservation, modelRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListEndpoints(ctx)
			return collect(items, endpointObservation, endpointRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListEndpointConfigs(ctx)
			return collect(items, endpointConfigObservation, endpointConfigRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListTrainingJobs(ctx)
			return collect(items, trainingJobObservation, trainingJobRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListProcessingJobs(ctx)
			return collect(items, processingJobObservation, noRelationships[ProcessingJob], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListTransformJobs(ctx)
			return collect(items, transformJobObservation, noRelationships[TransformJob], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListHyperParameterTuningJobs(ctx)
			return collect(items, tuningJobObservation, noRelationships[HyperParameterTuningJob], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListProjects(ctx)
			return collect(items, projectObservation, noRelationships[Project], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListPipelines(ctx)
			return collect(items, pipelineObservation, noRelationships[Pipeline], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListFeatureGroups(ctx)
			return collect(items, featureGroupObservation, noRelationships[FeatureGroup], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListDomains(ctx)
			return collect(items, domainObservation, domainRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListUserProfiles(ctx)
			return collect(items, userProfileObservation, userProfileRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListApps(ctx)
			return collect(items, appObservation, noRelationships[App], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListInferenceComponents(ctx)
			return collect(items, inferenceComponentObservation, noRelationships[InferenceComponent], err)
		},
	}
}

// collect maps one resource group through its observation and relationship
// builders, propagating any list error first so a partial scan never emits
// truncated truth.
func collect[T any](
	items []T,
	observe func(T) awscloud.ResourceObservation,
	relate func(T) []awscloud.RelationshipObservation,
	err error,
) ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
	if err != nil {
		return nil, nil, err
	}
	observations := make([]awscloud.ResourceObservation, 0, len(items))
	var relationships []awscloud.RelationshipObservation
	for _, item := range items {
		observations = append(observations, observe(item))
		relationships = append(relationships, relate(item)...)
	}
	return observations, relationships, nil
}

// noRelationships is the relationship builder for resource groups that emit
// resources only. It keeps the stage table uniform without per-type closures.
func noRelationships[T any](T) []awscloud.RelationshipObservation { return nil }
