// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bedrock

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Bedrock control-plane metadata facts for one claimed
// account and region. It never invokes a model (bedrock-runtime), never queries
// a knowledge base or agent (bedrock-agent-runtime), never mutates Bedrock
// state, and never persists agent instructions, prompt-override configurations,
// guardrail topic or content policy bodies, knowledge base ingested document
// content, or action-group API schema bodies.
type Scanner struct {
	Client Client
}

// Scan observes the in-scope Bedrock resource types through the configured
// client and returns aws_resource and aws_relationship fact envelopes. It
// returns an error when the client is missing or the boundary names a
// non-Bedrock service kind.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("bedrock scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceBedrock:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceBedrock
	default:
		return nil, fmt.Errorf("bedrock scanner received service_kind %q", boundary.ServiceKind)
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
			items, err := s.Client.ListFoundationModels(ctx)
			return collect(items, foundationModelObservation, noRelationships[FoundationModel], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListCustomModels(ctx)
			return collect(items, customModelObservation, customModelRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListModelCustomizationJobs(ctx)
			return collect(items, customizationJobObservation, noRelationships[ModelCustomizationJob], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListProvisionedModelThroughputs(ctx)
			return collect(items, provisionedThroughputObservation, provisionedThroughputRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListGuardrails(ctx)
			return collect(items, guardrailObservation, noRelationships[Guardrail], err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListAgents(ctx)
			return collect(items, agentObservation, agentRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListAgentActionGroups(ctx)
			return collect(items, actionGroupObservation, actionGroupRelationships, err)
		},
		func() ([]awscloud.ResourceObservation, []awscloud.RelationshipObservation, error) {
			items, err := s.Client.ListKnowledgeBases(ctx)
			return collect(items, knowledgeBaseObservation, knowledgeBaseRelationships, err)
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
