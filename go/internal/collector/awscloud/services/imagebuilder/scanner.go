// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imagebuilder

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits EC2 Image Builder metadata-only facts for one claimed account
// and region. It reports image pipelines, image recipes, container recipes,
// infrastructure configurations, and distribution configurations plus the
// cross-resource edges those resources report. It never reads or persists
// component build-document bodies, Dockerfile template bodies, instance user
// data, or any build artifact, and never mutates Image Builder state.
type Scanner struct {
	// Client is the metadata-only Image Builder snapshot source.
	Client Client
}

// Scan observes Image Builder pipelines, recipes, container recipes,
// infrastructure configurations, and distribution configurations through the
// configured client and emits their resource and relationship facts.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("imagebuilder scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceImageBuilder:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceImageBuilder
	default:
		return nil, fmt.Errorf("imagebuilder scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Image Builder resources: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	if err := appendPipelines(&envelopes, boundary, snapshot.Pipelines); err != nil {
		return nil, err
	}
	if err := appendImageRecipes(&envelopes, boundary, snapshot.ImageRecipes); err != nil {
		return nil, err
	}
	if err := appendContainerRecipes(&envelopes, boundary, snapshot.ContainerRecipes); err != nil {
		return nil, err
	}
	if err := appendInfraConfigs(&envelopes, boundary, snapshot.InfrastructureConfigurations); err != nil {
		return nil, err
	}
	if err := appendDistributionConfigs(&envelopes, boundary, snapshot.DistributionConfigurations); err != nil {
		return nil, err
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

func appendPipelines(envelopes *[]facts.Envelope, boundary awscloud.Boundary, pipelines []ImagePipeline) error {
	for _, pipeline := range pipelines {
		resource, err := awscloud.NewResourceEnvelope(pipelineObservation(boundary, pipeline))
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, resource)
		if err := appendRelationships(envelopes, pipelineRelationships(boundary, pipeline)); err != nil {
			return err
		}
	}
	return nil
}

func appendImageRecipes(envelopes *[]facts.Envelope, boundary awscloud.Boundary, recipes []ImageRecipe) error {
	for _, recipe := range recipes {
		resource, err := awscloud.NewResourceEnvelope(imageRecipeObservation(boundary, recipe))
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, resource)
	}
	return nil
}

func appendContainerRecipes(envelopes *[]facts.Envelope, boundary awscloud.Boundary, recipes []ContainerRecipe) error {
	for _, recipe := range recipes {
		resource, err := awscloud.NewResourceEnvelope(containerRecipeObservation(boundary, recipe))
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, resource)
		if err := appendRelationships(envelopes, containerRecipeRelationships(boundary, recipe)); err != nil {
			return err
		}
	}
	return nil
}

func appendInfraConfigs(envelopes *[]facts.Envelope, boundary awscloud.Boundary, configs []InfrastructureConfiguration) error {
	for _, config := range configs {
		resource, err := awscloud.NewResourceEnvelope(infraConfigObservation(boundary, config))
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, resource)
		if err := appendRelationships(envelopes, infraConfigRelationships(boundary, config)); err != nil {
			return err
		}
	}
	return nil
}

func appendDistributionConfigs(envelopes *[]facts.Envelope, boundary awscloud.Boundary, configs []DistributionConfiguration) error {
	for _, config := range configs {
		resource, err := awscloud.NewResourceEnvelope(distributionConfigObservation(boundary, config))
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, resource)
	}
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
